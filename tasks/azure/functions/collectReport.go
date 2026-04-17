package functions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	log "github.com/newrelic/newrelic-diagnostics-cli/logger"
	"github.com/newrelic/newrelic-diagnostics-cli/tasks"
)

const reportFilename = "azure-functions-report.txt"

// AzureFunctionsCollectReport aggregates all Azure Functions diagnostic task
// results into a human-readable text report and saves it to the output directory.
type AzureFunctionsCollectReport struct{}

// Identifier returns the task identifier.
func (t AzureFunctionsCollectReport) Identifier() tasks.Identifier {
	return tasks.IdentifierFromString("Azure/Functions/CollectReport")
}

// Explain returns the help text for this task.
func (t AzureFunctionsCollectReport) Explain() string {
	return "Aggregate Azure Functions diagnostic results into a text report"
}

// Dependencies returns the upstream diagnostic tasks whose results are included
// in the report.
func (t AzureFunctionsCollectReport) Dependencies() []string {
	return []string{
		taskDetectFunctionApp,
		taskFetchAppSettings,
		"Azure/Functions/DetectRuntime",
		"Azure/Functions/ValidateAgentConfig",
		"Azure/Functions/CheckCrashDumpConfig",
		"Azure/Functions/AgentInfo",
		"Azure/Functions/AnalyzeLogs",
	}
}

// Execute builds the diagnostic report from upstream results and writes it to
// nrdiag-output/azure-functions-report.txt.
func (t AzureFunctionsCollectReport) Execute(options tasks.Options, upstream map[string]tasks.Result) tasks.Result {
	remoteOK := upstream[taskFetchAppSettings].Status == tasks.Info
	localOK := upstream[taskDetectFunctionApp].Status == tasks.Info
	if !remoteOK && !localOK {
		return tasks.Result{
			Status:  tasks.None,
			Summary: "Not running in an Azure Function App and no remote settings available; this task did not run",
		}
	}

	outputDir := options.Options["outputPath"]
	if outputDir == "" {
		outputDir = defaultOutputDir
	}

	report := buildReport(upstream)

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return tasks.Result{
			Status:  tasks.Error,
			Summary: fmt.Sprintf("Failed to create output directory %s: %s", outputDir, err.Error()),
		}
	}

	reportPath := filepath.Join(outputDir, reportFilename)
	if err := os.WriteFile(reportPath, []byte(report), 0644); err != nil {
		log.Debug("Azure/Functions/CollectReport: failed to write report: " + err.Error())
		return tasks.Result{
			Status:  tasks.Error,
			Summary: fmt.Sprintf("Failed to write report to %s: %s", reportPath, err.Error()),
		}
	}

	return tasks.Result{
		Status:  tasks.Info,
		Summary: fmt.Sprintf("Diagnostic report saved to %s", reportPath),
		Payload: reportPath,
		FilesToCopy: []tasks.FileCopyEnvelope{
			{Path: reportPath, Identifier: "Azure/Functions/CollectReport"},
		},
	}
}

// buildReport formats all upstream results into a readable text report.
func buildReport(upstream map[string]tasks.Result) string {
	var sb strings.Builder

	sb.WriteString("=== Azure Functions New Relic Diagnostic Report ===\n")
	sb.WriteString(fmt.Sprintf("Generated : %s\n", time.Now().UTC().Format(time.RFC3339)))
	sb.WriteString(strings.Repeat("=", 51) + "\n\n")

	order := []string{
		taskDetectFunctionApp,
		taskFetchAppSettings,
		"Azure/Functions/DetectRuntime",
		"Azure/Functions/ValidateAgentConfig",
		"Azure/Functions/CheckCrashDumpConfig",
		"Azure/Functions/AgentInfo",
		"Azure/Functions/AnalyzeLogs",
	}

	for _, id := range order {
		result, ok := upstream[id]
		if !ok {
			continue
		}
		const prefix = "Azure/Functions/"
		name := id
		if len(id) > len(prefix) {
			name = id[len(prefix):]
		}
		sb.WriteString(fmt.Sprintf("[%-22s] %-8s - %s\n", name, statusLabel(result.Status), result.Summary))
	}

	sb.WriteString("\n" + strings.Repeat("-", 51) + "\n")
	sb.WriteString("End of report. See nrdiag-output.zip for all collected files.\n")

	return sb.String()
}

// statusLabel returns a short human-readable label for a task status.
func statusLabel(s tasks.Status) string {
	switch s {
	case tasks.Success:
		return "Success"
	case tasks.Warning:
		return "Warning"
	case tasks.Failure:
		return "Failure"
	case tasks.Error:
		return "Error"
	case tasks.Info:
		return "Info"
	default:
		return "None"
	}
}
