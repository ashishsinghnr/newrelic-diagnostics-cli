package functions

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	log "github.com/newrelic/newrelic-diagnostics-cli/logger"
	"github.com/newrelic/newrelic-diagnostics-cli/tasks"
)

const processDetailTimeoutSeconds = 30

// kuduProcessDetail maps the JSON response from GET /api/processes/{id}.
// Field names and casing match the Kudu ProcessInfo DTO exactly (source:
// github.com/projectkudu/kudu/blob/master/Kudu.Contracts/Diagnostics/ProcessInfo.cs).
// Array/map sections are kept as raw JSON so they can be written directly to
// files without a second round-trip.
type kuduProcessDetail struct {
	ID                   int             `json:"id"`
	Name                 string          `json:"name"`
	MachineName          string          `json:"machineName"` // camelCase — Kudu DTO inconsistency
	FileName             string          `json:"file_name"`
	CommandLine          string          `json:"command_line"`
	UserName             string          `json:"user_name"`
	Description          string          `json:"description"`
	IsScmSite            bool            `json:"is_scm_site"`
	IsWebJob             bool            `json:"is_webjob"`
	Minidump             string          `json:"minidump"` // URL to request a memory dump
	HandleCount          int             `json:"handle_count"`
	ModuleCount          int             `json:"module_count"`
	ThreadCount          int             `json:"thread_count"`
	StartTime            string          `json:"start_time"`
	TimeStamp            string          `json:"time_stamp"`
	TotalCPUTime         string          `json:"total_cpu_time"`
	UserCPUTime          string          `json:"user_cpu_time"`
	PrivilegedCPUTime    string          `json:"privileged_cpu_time"`
	WorkingSet           int64           `json:"working_set"`
	PeakWorkingSet       int64           `json:"peak_working_set"`
	PrivateMemory        int64           `json:"private_memory"`
	VirtualMemory        int64           `json:"virtual_memory"`
	PeakVirtualMemory    int64           `json:"peak_virtual_memory"`
	PagedMemory          int64           `json:"paged_memory"`
	PeakPagedMemory      int64           `json:"peak_paged_memory"`
	PagedSystemMemory    int64           `json:"paged_system_memory"`
	NonPagedSystemMemory int64           `json:"non_paged_system_memory"`
	Modules              json.RawMessage `json:"modules"`
	Threads              json.RawMessage `json:"threads"`
	OpenFileHandles      json.RawMessage `json:"open_file_handles"`
	EnvironmentVariables json.RawMessage `json:"environment_variables"`
}

// ProcessDetailsResult holds the output file paths for the collected process
// property files.
type ProcessDetailsResult struct {
	FunctionAppName string
	ProcessID       int
	ProcessName     string
	Files           []string
}

// AzureFunctionsCollectProcessDetails interactively collects all Kudu process
// property tabs — General, Modules, Handles, Threads, and Environment
// Variables — for a selected process and saves each as a JSON file.
type AzureFunctionsCollectProcessDetails struct {
	// cmdRunner and httpClient are injectable for tests.
	cmdRunner  func(name string, args ...string) ([]byte, error)
	httpClient *http.Client
}

// Identifier returns the task identifier.
func (t AzureFunctionsCollectProcessDetails) Identifier() tasks.Identifier {
	return tasks.IdentifierFromString("Azure/Functions/CollectProcessDetails")
}

// Explain returns the help text for this task.
func (t AzureFunctionsCollectProcessDetails) Explain() string {
	return "Collect process property details (general, modules, handles, threads, environment variables) from an Azure Function App via Kudu"
}

// Dependencies ensures this task runs after the site dump is available.
func (t AzureFunctionsCollectProcessDetails) Dependencies() []string {
	return []string{
		taskDownloadSiteDump,
	}
}

// Execute prompts the user to collect process details. If confirmed, it
// authenticates, lists running processes, prompts for a PID, fetches all
// property tabs from GET /api/processes/{pid}, and saves one JSON file per tab.
func (t AzureFunctionsCollectProcessDetails) Execute(options tasks.Options, upstream map[string]tasks.Result) tasks.Result {
	funcName, resourceGroup := resolveFunctionTarget(options, upstream)
	if funcName == "" || resourceGroup == "" {
		return tasks.Result{
			Status:  tasks.None,
			Summary: "Skipped: functionName and resourceGroup options are required",
		}
	}

	time.Sleep(promptFlushDelay)
	if !tasks.PromptUser("Do you want to collect process property details (general, modules, handles, threads, environment variables)?", options) {
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
		client = &http.Client{Timeout: processDetailTimeoutSeconds * time.Second}
	}

	scmURL := fmt.Sprintf("https://%s.scm.azurewebsites.net", url.PathEscape(funcName))

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

	detail, err := fetchProcessDetail(client, scmURL, authHeader, pid)
	if err != nil {
		log.Debug("Azure/Functions/CollectProcessDetails: fetch failed: " + err.Error())
		return tasks.Result{
			Status:  tasks.Error,
			Summary: fmt.Sprintf("Failed to fetch process details for PID %d: %s", pid, err.Error()),
			URL:     kuduDocsURL,
		}
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return tasks.Result{
			Status:  tasks.Error,
			Summary: fmt.Sprintf("Failed to create output directory %s: %s", outputDir, err.Error()),
		}
	}

	files, err := saveProcessDetails(detail, outputDir, funcName)
	if err != nil {
		return tasks.Result{
			Status:  tasks.Error,
			Summary: fmt.Sprintf("Failed to save process details: %s", err.Error()),
		}
	}

	envelopes := make([]tasks.FileCopyEnvelope, len(files))
	for i, f := range files {
		envelopes[i] = tasks.FileCopyEnvelope{Path: f, Identifier: "Azure/Functions/CollectProcessDetails"}
	}

	return tasks.Result{
		Status:  tasks.Info,
		Summary: fmt.Sprintf("Process details for %s (PID %d) saved to %s (%d files)", procName, pid, outputDir, len(files)),
		Payload: &ProcessDetailsResult{
			FunctionAppName: funcName,
			ProcessID:       pid,
			ProcessName:     procName,
			Files:           files,
		},
		FilesToCopy: envelopes,
		URL:         kuduDocsURL,
	}
}

// fetchProcessDetail calls GET /api/processes/{pid} on the Kudu SCM endpoint.
func fetchProcessDetail(client *http.Client, scmURL, authHeader string, pid int) (*kuduProcessDetail, error) {
	url := fmt.Sprintf("%s%s%d", scmURL, kuduProcessesEndpoint, pid)
	req, err := http.NewRequest(http.MethodGet, url, nil)
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

	if resp.StatusCode == http.StatusBadRequest {
		return nil, fmt.Errorf("process API returned HTTP 400 for PID %d — this feature is not supported on Linux App Service; it requires Windows App Service or a compatible Azure Functions plan", pid)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Kudu returned HTTP %d for process %d", resp.StatusCode, pid)
	}

	var detail kuduProcessDetail
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		return nil, fmt.Errorf("failed to parse process detail JSON: %w", err)
	}
	return &detail, nil
}

// saveProcessDetails writes one pretty-printed JSON file per Kudu process tab.
// Returns the list of written file paths.
func saveProcessDetails(detail *kuduProcessDetail, outputDir, funcName string) ([]string, error) {
	prefix := fmt.Sprintf("%s-pid%d", funcName, detail.ID)

	sections := []struct {
		suffix string
		data   any
	}{
		{"general", buildGeneralSection(detail)},
		{"modules", detail.Modules},
		{"handles", detail.OpenFileHandles},
		{"threads", detail.Threads},
		{"environment", detail.EnvironmentVariables},
	}

	var saved []string
	for _, s := range sections {
		b, err := json.MarshalIndent(s.data, "", "  ")
		if err != nil {
			return saved, fmt.Errorf("failed to marshal %s section: %w", s.suffix, err)
		}
		path := filepath.Join(outputDir, fmt.Sprintf("%s-%s.json", prefix, s.suffix))
		if err := os.WriteFile(path, b, 0644); err != nil {
			return saved, fmt.Errorf("failed to write %s: %w", path, err)
		}
		saved = append(saved, path)
	}
	return saved, nil
}

// buildGeneralSection extracts all scalar fields from the process detail into
// a map that becomes the "general" tab JSON file. Memory fields are renamed
// with a _bytes suffix for clarity since the Kudu API omits units.
func buildGeneralSection(d *kuduProcessDetail) map[string]any {
	return map[string]any{
		"id":                             d.ID,
		"name":                           d.Name,
		"machine_name":                   d.MachineName,
		"file_name":                      d.FileName,
		"command_line":                   d.CommandLine,
		"user_name":                      d.UserName,
		"description":                    d.Description,
		"is_scm_site":                    d.IsScmSite,
		"is_webjob":                      d.IsWebJob,
		"minidump_url":                   d.Minidump,
		"handle_count":                   d.HandleCount,
		"module_count":                   d.ModuleCount,
		"thread_count":                   d.ThreadCount,
		"start_time":                     d.StartTime,
		"time_stamp":                     d.TimeStamp,
		"total_cpu_time":                 d.TotalCPUTime,
		"user_cpu_time":                  d.UserCPUTime,
		"privileged_cpu_time":            d.PrivilegedCPUTime,
		"working_set_bytes":              d.WorkingSet,
		"peak_working_set_bytes":         d.PeakWorkingSet,
		"private_memory_bytes":           d.PrivateMemory,
		"virtual_memory_bytes":           d.VirtualMemory,
		"peak_virtual_memory_bytes":      d.PeakVirtualMemory,
		"paged_memory_bytes":             d.PagedMemory,
		"peak_paged_memory_bytes":        d.PeakPagedMemory,
		"paged_system_memory_bytes":      d.PagedSystemMemory,
		"non_paged_system_memory_bytes":  d.NonPagedSystemMemory,
	}
}
