package functions

import (
	"strings"

	log "github.com/newrelic/newrelic-diagnostics-cli/logger"
	"github.com/newrelic/newrelic-diagnostics-cli/tasks"
)

// knownRuntimes are the valid values for FUNCTIONS_WORKER_RUNTIME.
var knownRuntimes = map[string]bool{
	"dotnet":          true,
	"dotnet-isolated": true,
	"node":            true,
	"python":          true,
	"java":            true,
	"powershell":      true,
	"custom":          true,
}

// AzureFunctionsDetectRuntime identifies the language runtime of the Azure Function App.
type AzureFunctionsDetectRuntime struct{}

// Identifier returns the task identifier.
func (t AzureFunctionsDetectRuntime) Identifier() tasks.Identifier {
	return tasks.IdentifierFromString("Azure/Functions/DetectRuntime")
}

// Explain returns the help text for this task.
func (t AzureFunctionsDetectRuntime) Explain() string {
	return "Detect the language runtime of the Azure Function App (dotnet-isolated, node, python, java, etc.)"
}

// Dependencies returns the upstream tasks this task depends on.
func (t AzureFunctionsDetectRuntime) Dependencies() []string {
	return []string{
		taskDetectFunctionApp,
		taskFetchAppSettings,
	}
}

// Execute reads FUNCTIONS_WORKER_RUNTIME from either the remote FetchAppSettings
// (local machine targeting Azure) or the local DetectFunctionApp (inside container).
func (t AzureFunctionsDetectRuntime) Execute(options tasks.Options, upstream map[string]tasks.Result) tasks.Result {
	// Try remote app settings first (running from local machine).
	var funcEnvVars map[string]string
	if upstream[taskFetchAppSettings].Status == tasks.Info {
		if settings, ok := upstream[taskFetchAppSettings].Payload.(map[string]string); ok {
			funcEnvVars = settings
		}
	}

	// Fall back to local env vars (running inside Azure container).
	if funcEnvVars == nil {
		if upstream[taskDetectFunctionApp].Status != tasks.Info {
			return tasks.Result{
				Status:  tasks.None,
				Summary: "Not running in an Azure Function App and no remote settings available; this task did not run",
			}
		}
		var ok bool
		funcEnvVars, ok = upstream[taskDetectFunctionApp].Payload.(map[string]string)
		if !ok {
			log.Debug("Azure/Functions/DetectRuntime: could not cast DetectFunctionApp payload")
			return tasks.Result{
				Status:  tasks.Error,
				Summary: "Unable to read Function App environment variables from upstream task",
			}
		}
	}

	runtime := strings.ToLower(funcEnvVars["FUNCTIONS_WORKER_RUNTIME"])
	if runtime == "" {
		return tasks.Result{
			Status:  tasks.Warning,
			Summary: "FUNCTIONS_WORKER_RUNTIME is empty; cannot determine function runtime",
		}
	}

	if !knownRuntimes[runtime] {
		return tasks.Result{
			Status:  tasks.Warning,
			Summary: "Unrecognised FUNCTIONS_WORKER_RUNTIME value: " + runtime,
			Payload: runtime,
		}
	}

	return tasks.Result{
		Status:  tasks.Info,
		Summary: "Detected Azure Function App runtime: " + runtime,
		Payload: runtime,
	}
}

// IsDotnetRuntime returns true when the runtime string represents a .NET-based function.
func IsDotnetRuntime(runtime string) bool {
	r := strings.ToLower(runtime)
	return r == "dotnet" || r == "dotnet-isolated"
}

// IsJavaRuntime returns true when the runtime string represents a Java function.
func IsJavaRuntime(runtime string) bool {
	return strings.ToLower(runtime) == "java"
}

// IsNodeRuntime returns true when the runtime string represents a Node.js function.
func IsNodeRuntime(runtime string) bool {
	return strings.ToLower(runtime) == "node"
}

// IsPythonRuntime returns true when the runtime string represents a Python function.
func IsPythonRuntime(runtime string) bool {
	return strings.ToLower(runtime) == "python"
}
