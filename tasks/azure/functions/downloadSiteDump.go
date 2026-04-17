package functions

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	log "github.com/newrelic/newrelic-diagnostics-cli/logger"
	"github.com/newrelic/newrelic-diagnostics-cli/tasks"
)

const (
	kuduDumpEndpoint   = "/api/dump"
	defaultOutputDir   = "nrdiag-output"
	kuduTimeoutSeconds = 600
)

// SiteDumpResult holds metadata about the downloaded site dump.
type SiteDumpResult struct {
	FunctionAppName string
	ResourceGroup   string
	OutputPath      string
	SizeMB          float64
}

// AzureFunctionsDownloadSiteDump downloads a Kudu site diagnostic zip from
// the Azure Function App's SCM endpoint. It runs on the developer's machine
// (not inside the container) and authenticates via the az CLI.
type AzureFunctionsDownloadSiteDump struct {
	// cmdRunner executes shell commands; injectable for tests.
	cmdRunner func(name string, args ...string) ([]byte, error)
	// httpClient performs HTTP requests; injectable for tests.
	httpClient *http.Client
}

// Identifier returns the task identifier.
func (t AzureFunctionsDownloadSiteDump) Identifier() tasks.Identifier {
	return tasks.IdentifierFromString("Azure/Functions/DownloadSiteDump")
}

// Explain returns the help text for this task.
func (t AzureFunctionsDownloadSiteDump) Explain() string {
	return "Download a Kudu site diagnostic zip from an Azure Function App SCM endpoint"
}

// Dependencies returns no upstream tasks — this task runs from the developer
// machine and does not depend on in-container environment detection.
func (t AzureFunctionsDownloadSiteDump) Dependencies() []string {
	return []string{}
}

// Execute downloads the Kudu site dump zip file. It requires the functionName
// and resourceGroup options to be set. An optional outputPath option overrides
// the default output directory.
func (t AzureFunctionsDownloadSiteDump) Execute(options tasks.Options, upstream map[string]tasks.Result) tasks.Result {
	funcName := options.Options["functionName"]
	resourceGroup := options.Options["resourceGroup"]

	if funcName == "" || resourceGroup == "" {
		return tasks.Result{
			Status: tasks.None,
			Summary: "Skipped: functionName and resourceGroup options are required. " +
				"Pass them with -override Azure/Functions/DownloadSiteDump.functionName=<name> " +
				"-override Azure/Functions/DownloadSiteDump.resourceGroup=<rg>",
		}
	}

	outputDir := options.Options["outputPath"]
	if outputDir == "" {
		outputDir = defaultOutputDir
	}

	runner := t.cmdRunner
	if runner == nil {
		runner = defaultCmdRunner
	}

	client := t.httpClient
	if client == nil {
		client = &http.Client{Timeout: kuduTimeoutSeconds * time.Second}
	}

	scmURL := fmt.Sprintf("https://%s.scm.azurewebsites.net", funcName)

	authHeader, err := buildAuthHeader(runner, funcName, resourceGroup)
	if err != nil {
		return tasks.Result{
			Status:  tasks.Error,
			Summary: fmt.Sprintf("Failed to authenticate with Azure: %s", err.Error()),
			URL:     kuduDocsURL,
		}
	}

	dumpURL := scmURL + kuduDumpEndpoint
	log.Debug("Azure/Functions/DownloadSiteDump: fetching " + dumpURL)
	log.Info("Verifying agent config and downloading diagnostic dump folder containing logs...")

	req, err := http.NewRequest(http.MethodGet, dumpURL, nil)
	if err != nil {
		return tasks.Result{
			Status:  tasks.Error,
			Summary: fmt.Sprintf("Failed to create HTTP request: %s", err.Error()),
		}
	}
	req.Header.Set("Authorization", authHeader)

	resp, err := client.Do(req)
	if err != nil {
		return tasks.Result{
			Status:  tasks.Error,
			Summary: fmt.Sprintf("Failed to connect to Kudu endpoint %s: %s", dumpURL, err.Error()),
			URL:     kuduDocsURL,
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return tasks.Result{
			Status:  tasks.Failure,
			Summary: fmt.Sprintf("Kudu endpoint returned HTTP %d for %s", resp.StatusCode, dumpURL),
			URL:     kuduDocsURL,
		}
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return tasks.Result{
			Status:  tasks.Error,
			Summary: fmt.Sprintf("Failed to create output directory %s: %s", outputDir, err.Error()),
		}
	}

	filename := fmt.Sprintf("%s-site-dump.zip", funcName)
	outputPath := filepath.Join(outputDir, filename)

	written, err := saveStream(resp.Body, outputPath)
	if err != nil {
		return tasks.Result{
			Status:  tasks.Error,
			Summary: fmt.Sprintf("Failed to save dump to %s: %s", outputPath, err.Error()),
		}
	}

	sizeMB := float64(written) / 1024 / 1024
	result := &SiteDumpResult{
		FunctionAppName: funcName,
		ResourceGroup:   resourceGroup,
		OutputPath:      outputPath,
		SizeMB:          sizeMB,
	}

	return tasks.Result{
		Status:  tasks.Info,
		Summary: fmt.Sprintf("Site dump downloaded to %s (%.2f MB)", outputPath, sizeMB),
		Payload: result,
		FilesToCopy: []tasks.FileCopyEnvelope{
			{Path: outputPath, Identifier: "Azure/Functions/DownloadSiteDump"},
		},
		URL: kuduDocsURL,
	}
}

// buildAuthHeader tries Bearer token auth first, then falls back to Basic auth
// using publishing credentials — mirroring the Python kudu.py approach.
func buildAuthHeader(runner func(string, ...string) ([]byte, error), funcName, resourceGroup string) (string, error) {
	// Try Bearer token via az account get-access-token.
	token, err := getBearerToken(runner)
	if err == nil && token != "" {
		return "Bearer " + token, nil
	}
	log.Debug("Azure/Functions/DownloadSiteDump: bearer token unavailable, trying basic auth: " + fmt.Sprintf("%v", err))

	// Fall back to Basic auth via publishing credentials.
	username, password, err := getPublishingCredentials(runner, funcName, resourceGroup)
	if err != nil {
		return "", fmt.Errorf("bearer token failed and basic auth failed: %w", err)
	}

	creds := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	return "Basic " + creds, nil
}

// getBearerToken fetches an Azure management-plane bearer token via az CLI.
func getBearerToken(runner func(string, ...string) ([]byte, error)) (string, error) {
	out, err := runner("az", "account", "get-access-token",
		"--resource", "https://management.azure.com/",
		"--query", "accessToken",
		"-o", "tsv",
	)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return "", fmt.Errorf("az account get-access-token returned empty token")
	}
	return token, nil
}

// publishingCredentials is the JSON shape returned by az functionapp deployment
// list-publishing-credentials.
type publishingCredentials struct {
	PublishingUserName string `json:"publishingUserName"`
	PublishingPassword string `json:"publishingPassword"`
}

// getPublishingCredentials fetches Basic-auth credentials via az CLI.
func getPublishingCredentials(runner func(string, ...string) ([]byte, error), funcName, resourceGroup string) (string, string, error) {
	out, err := runner("az", "functionapp", "deployment", "list-publishing-credentials",
		"--name", funcName,
		"--resource-group", resourceGroup,
		"--query", "{publishingUserName:publishingUserName, publishingPassword:publishingPassword}",
		"-o", "json",
	)
	if err != nil {
		return "", "", fmt.Errorf("az functionapp deployment list-publishing-credentials: %w", err)
	}

	var creds publishingCredentials
	if err := json.Unmarshal(out, &creds); err != nil {
		return "", "", fmt.Errorf("failed to parse publishing credentials JSON: %w", err)
	}
	if creds.PublishingUserName == "" || creds.PublishingPassword == "" {
		return "", "", fmt.Errorf("publishing credentials are empty")
	}
	return creds.PublishingUserName, creds.PublishingPassword, nil
}

// saveStream writes the response body to dst and returns the number of bytes written.
func saveStream(src io.Reader, dst string) (int64, error) {
	f, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return io.Copy(f, src)
}

// defaultCmdRunner shells out to run a command and returns its combined output.
func defaultCmdRunner(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}
