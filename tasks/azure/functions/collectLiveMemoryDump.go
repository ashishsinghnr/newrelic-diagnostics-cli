package functions

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	log "github.com/newrelic/newrelic-diagnostics-cli/logger"
	"github.com/newrelic/newrelic-diagnostics-cli/tasks"
)

const (
	kuduProcessesEndpoint = "/api/processes/"
	memDumpTimeoutSeconds = 600
	kuduDocsURL           = "https://learn.microsoft.com/en-us/azure/app-service/resources-kudu"
)

// kuduProcess represents a single entry from the Kudu /api/processes/ response.
type kuduProcess struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Href string `json:"href"`
}

// MemoryDumpResult holds metadata about the collected live memory dump.
type MemoryDumpResult struct {
	FunctionAppName string
	ProcessID       int
	ProcessName     string
	OutputPath      string
	SizeMB          float64
}

// AzureFunctionsCollectLiveMemoryDump interactively prompts the user to collect
// a live full memory dump of the running Azure Function App process via Kudu.
type AzureFunctionsCollectLiveMemoryDump struct {
	// cmdRunner and httpClient are injectable for tests.
	cmdRunner  func(name string, args ...string) ([]byte, error)
	httpClient *http.Client
}

// Identifier returns the task identifier.
func (t AzureFunctionsCollectLiveMemoryDump) Identifier() tasks.Identifier {
	return tasks.IdentifierFromString("Azure/Functions/CollectLiveMemoryDump")
}

// Explain returns the help text for this task.
func (t AzureFunctionsCollectLiveMemoryDump) Explain() string {
	return "Interactively collect a live full memory dump from an Azure Function App via Kudu"
}

// Dependencies ensures this task runs after process details are collected,
// so the process-details prompt appears before the memory dump prompt.
// taskDownloadSiteDump is listed explicitly so its result is available in
// upstream for resolveFunctionTarget to fall back to.
func (t AzureFunctionsCollectLiveMemoryDump) Dependencies() []string {
	return []string{
		taskCollectProcessDetails,
		taskDownloadSiteDump,
	}
}

// Execute prompts the user to collect a live memory dump. If confirmed, it
// authenticates via the az CLI, discovers the dotnet process via Kudu, and
// streams the memory dump to disk.
func (t AzureFunctionsCollectLiveMemoryDump) Execute(options tasks.Options, upstream map[string]tasks.Result) tasks.Result {
	funcName, resourceGroup := resolveFunctionTarget(options, upstream)
	if funcName == "" || resourceGroup == "" {
		return tasks.Result{
			Status:  tasks.None,
			Summary: "Skipped: functionName and resourceGroup options are required",
		}
	}

	if err := validateAzureTarget(funcName, resourceGroup); err != nil {
		return tasks.Result{
			Status:  tasks.Error,
			Summary: err.Error(),
		}
	}

	time.Sleep(promptFlushDelay)
	if !tasks.PromptUser("Do you want to collect a Full Memory dump right now?", options) {
		return tasks.Result{
			Status:  tasks.None,
			Summary: "Skipped by user",
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
		client = &http.Client{Timeout: memDumpTimeoutSeconds * time.Second}
	}

	scmURL, err := buildScmURL(funcName)
	if err != nil {
		return tasks.Result{
			Status:  tasks.Error,
			Summary: err.Error(),
		}
	}

	authHeader, err := buildAuthHeader(runner, funcName, resourceGroup)
	if err != nil {
		return tasks.Result{
			Status:  tasks.Error,
			Summary: fmt.Sprintf("Failed to authenticate with Azure: %s", err.Error()),
			URL:     kuduDocsURL,
		}
	}

	pid, procName, pidErr := resolveProcess(options, client, scmURL, authHeader)
	if pidErr != nil {
		return *pidErr
	}

	dumpURL := fmt.Sprintf("%s%s%d/dump?dumpType=2", scmURL, kuduProcessesEndpoint, pid)
	log.Debug("Azure/Functions/CollectLiveMemoryDump: fetching " + dumpURL)

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
			Summary: fmt.Sprintf("Failed to connect to Kudu process dump endpoint: %s", err.Error()),
			URL:     kuduDocsURL,
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return tasks.Result{
			Status:  tasks.Failure,
			Summary: fmt.Sprintf("Kudu process dump endpoint returned HTTP %d", resp.StatusCode),
			URL:     kuduDocsURL,
		}
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return tasks.Result{
			Status:  tasks.Error,
			Summary: fmt.Sprintf("Failed to create output directory %s: %s", outputDir, err.Error()),
		}
	}

	filename := fmt.Sprintf("%s-memdump.dmp", funcName)
	outputPath := filepath.Join(outputDir, filename)

	written, err := saveStream(resp.Body, outputPath)
	if err != nil {
		return tasks.Result{
			Status:  tasks.Error,
			Summary: fmt.Sprintf("Failed to save memory dump to %s: %s", outputPath, err.Error()),
		}
	}

	sizeMB := float64(written) / 1024 / 1024
	result := &MemoryDumpResult{
		FunctionAppName: funcName,
		ProcessID:       pid,
		ProcessName:     procName,
		OutputPath:      outputPath,
		SizeMB:          sizeMB,
	}

	return tasks.Result{
		Status:  tasks.Info,
		Summary: fmt.Sprintf("Memory dump of process %s (PID %d) saved to %s (%.2f MB)", procName, pid, outputPath, sizeMB),
		Payload: result,
		FilesToCopy: []tasks.FileCopyEnvelope{
			{Path: outputPath, Identifier: "Azure/Functions/CollectLiveMemoryDump"},
		},
		URL: kuduDocsURL,
	}
}

// listProcesses fetches the running process list from the Kudu API.
func listProcesses(client *http.Client, scmURL, authHeader string) ([]kuduProcess, error) {
	req, err := http.NewRequest(http.MethodGet, scmURL+kuduProcessesEndpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusBadRequest:
		return nil, fmt.Errorf("process API returned HTTP 400 — not supported on Linux App Service; requires Windows App Service or a compatible Azure Functions plan")
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("Kudu /api/processes/ returned HTTP %d — Azure credentials likely missing or expired; run `az login` and retry", resp.StatusCode)
	default:
		return nil, fmt.Errorf("Kudu /api/processes/ returned HTTP %d", resp.StatusCode)
	}

	var processes []kuduProcess
	if err := json.NewDecoder(resp.Body).Decode(&processes); err != nil {
		return nil, fmt.Errorf("failed to parse process list: %w", err)
	}
	return processes, nil
}

// autoSelectProcess returns the first known runtime process from the list.
// It matches the canonical executable names for each supported runtime: dotnet,
// w3wp, func (.NET), node (Node.js), python (Python), and java (JVM). Matching
// uses the executable basename with any extension stripped, and accepts an
// optional trailing version suffix ("python3", "node20") so the common Linux
// names work. Non-runtime processes whose names happen to contain those
// substrings (e.g. "snode", "pythonpath-helper") are not selected.
func autoSelectProcess(processes []kuduProcess) (pid int, name string, found bool) {
	canonical := []string{"dotnet", "w3wp", "func", "node", "python", "java"}
	for _, p := range processes {
		base := strings.ToLower(p.Name)
		if i := strings.LastIndex(base, "."); i >= 0 {
			base = base[:i] // strip extension (e.g. ".exe")
		}
		for _, c := range canonical {
			if isCanonicalRuntimeName(base, c) {
				return p.ID, p.Name, true
			}
		}
	}
	return 0, "", false
}

// isCanonicalRuntimeName reports whether base equals canonical or canonical
// followed only by digits (e.g. "python", "python3", "node20"). It does not
// match canonical as a substring or with non-digit suffixes.
func isCanonicalRuntimeName(base, canonical string) bool {
	if base == canonical {
		return true
	}
	if !strings.HasPrefix(base, canonical) {
		return false
	}
	for _, r := range base[len(canonical):] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// promptProcessSelection displays the running process list and reads a PID
// choice from stdin.
func promptProcessSelection(processes []kuduProcess) (int, string, error) {
	fmt.Println("\nRunning processes in the Function App:")
	for _, p := range processes {
		fmt.Printf("  PID %-6d  %s\n", p.ID, p.Name)
	}
	fmt.Print("\nEnter the PID of the process to dump: ")

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		pid, err := strconv.Atoi(input)
		if err != nil || pid <= 0 {
			fmt.Printf("Invalid PID %q — please enter a numeric PID: ", input)
			continue
		}
		// Match a name from the list if possible.
		for _, p := range processes {
			if p.ID == pid {
				return pid, p.Name, nil
			}
		}
		// PID not in list — the user may know better (e.g. a child process).
		return pid, fmt.Sprintf("process(%d)", pid), nil
	}
	return 0, "", fmt.Errorf("no process selected")
}

// resolveProcess determines which process to dump:
//   - explicit processID override → use it directly (no Kudu call)
//   - YesToAll → auto-select first dotnet/w3wp/func from Kudu list
//   - interactive → show process list, prompt user to enter a PID
func resolveProcess(options tasks.Options, client *http.Client, scmURL, authHeader string) (int, string, *tasks.Result) {
	if pidStr := options.Options["processID"]; pidStr != "" {
		n, err := strconv.Atoi(pidStr)
		if err != nil {
			r := tasks.Result{
				Status:  tasks.Error,
				Summary: fmt.Sprintf("Invalid processID %q: must be an integer", pidStr),
			}
			return 0, "", &r
		}
		log.Debug(fmt.Sprintf("Azure/Functions/CollectLiveMemoryDump: using user-specified PID %d", n))
		return n, fmt.Sprintf("process(%d)", n), nil
	}

	processes, err := listProcesses(client, scmURL, authHeader)
	if err != nil {
		log.Debug("Azure/Functions/CollectLiveMemoryDump: process list failed: " + err.Error())
		r := tasks.Result{
			Status:  tasks.Error,
			Summary: fmt.Sprintf("Failed to list processes via Kudu: %s", err.Error()),
			URL:     kuduDocsURL,
		}
		return 0, "", &r
	}

	if options.Options["YesToAll"] == "true" {
		pid, name, found := autoSelectProcess(processes)
		if !found {
			available := make([]string, 0, len(processes))
			for _, p := range processes {
				available = append(available, fmt.Sprintf("%d:%s", p.ID, p.Name))
			}
			r := tasks.Result{
				Status: tasks.Error,
				Summary: fmt.Sprintf(
					"No known runtime process found (dotnet/w3wp/func/node/python/java); available: [%s]. "+
						"Use -override Azure/Functions/CollectLiveMemoryDump.processID=<pid>",
					strings.Join(available, ", "),
				),
				URL: kuduDocsURL,
			}
			return 0, "", &r
		}
		return pid, name, nil
	}

	pid, name, err := promptProcessSelection(processes)
	if err != nil {
		r := tasks.Result{Status: tasks.None, Summary: "No process selected"}
		return 0, "", &r
	}
	return pid, name, nil
}
