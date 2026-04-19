package functions

import (
	"fmt"
	"sort"
	"strings"

	log "github.com/newrelic/newrelic-diagnostics-cli/logger"
	"github.com/newrelic/newrelic-diagnostics-cli/tasks"
)

// sensitiveKeySuffixes lists suffixes that indicate a value should be masked.
var sensitiveKeySuffixes = []string{"_KEY", "_TOKEN", "_SECRET", "_PASSWORD", "_PASS", "_OBFUSCATED"}

// dotnetProfilerKeys are included for .NET runtimes in addition to NR vars.
// These match the required app settings for the New Relic .NET agent on Azure Functions (Linux):
// https://docs.newrelic.com/docs/serverless-function-monitoring/azure-function-monitoring/linux/#.net
var dotnetProfilerKeys = []string{
	"CORECLR_ENABLE_PROFILING",
	"CORECLR_NEWRELIC_HOME",
	"CORECLR_PROFILER",
	"CORECLR_PROFILER_PATH",
	"CORECLR_PROFILER_PATH_64", // architecture-specific fallback, collect if present
}

// AzureFunctionsAgentInfo collects all New Relic agent environment variables
// present in the Azure Function App and reports them with sensitive values masked.
type AzureFunctionsAgentInfo struct{}

// Identifier returns the task identifier.
func (t AzureFunctionsAgentInfo) Identifier() tasks.Identifier {
	return tasks.IdentifierFromString("Azure/Functions/AgentInfo")
}

// Explain returns the help text for this task.
func (t AzureFunctionsAgentInfo) Explain() string {
	return "Collect all New Relic agent environment variables from the Azure Function App"
}

// Dependencies returns the upstream tasks this task depends on.
func (t AzureFunctionsAgentInfo) Dependencies() []string {
	return []string{
		taskDetectFunctionApp,
		"Azure/Functions/DetectRuntime",
		"Base/Env/CollectEnvVars",
		taskFetchAppSettings,
	}
}

// Execute collects NR-related env vars and returns them as a masked map.
func (t AzureFunctionsAgentInfo) Execute(options tasks.Options, upstream map[string]tasks.Result) tasks.Result {
	envVars, ok := resolveEnvVars(upstream)
	if !ok {
		log.Debug("Azure/Functions/AgentInfo: no env vars available from remote or local")
		return tasks.Result{
			Status:  tasks.None,
			Summary: "Not running in an Azure Function App and no remote settings available; this task did not run",
		}
	}

	runtime, _ := upstream["Azure/Functions/DetectRuntime"].Payload.(string)

	collected := make(map[string]string)

	// Collect all NEW_RELIC_* and NEWRELIC_* env vars.
	for key, val := range envVars {
		upper := strings.ToUpper(key)
		if strings.HasPrefix(upper, "NEW_RELIC_") || strings.HasPrefix(upper, "NEWRELIC_") {
			collected[key] = maskIfSensitive(key, val)
		}
	}

	// For .NET runtimes also collect profiler env vars.
	if IsDotnetRuntime(runtime) {
		for _, key := range dotnetProfilerKeys {
			if val, present := envVars[key]; present {
				collected[key] = val // profiler vars are not sensitive
			}
		}
	}

	if len(collected) == 0 {
		return tasks.Result{
			Status:  tasks.Warning,
			Summary: "No New Relic agent environment variables found in this Azure Function App",
			URL:     "https://docs.newrelic.com/docs/serverless-function-monitoring/azure-functions/install/",
		}
	}

	return tasks.Result{
		Status:  tasks.Info,
		Summary: formatAgentInfoSummary(collected, runtime),
		Payload: collected,
	}
}

// maskIfSensitive returns a masked version of val when key indicates a secret.
func maskIfSensitive(key, val string) string {
	upper := strings.ToUpper(key)
	for _, suffix := range sensitiveKeySuffixes {
		if strings.HasSuffix(upper, suffix) {
			if len(val) > 4 {
				return val[:4] + "****"
			}
			return "****"
		}
	}
	return val
}

func formatAgentInfoSummary(vars map[string]string, runtime string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d New Relic agent setting(s)", len(vars)))
	if runtime != "" {
		sb.WriteString(fmt.Sprintf(" (runtime: %s)", runtime))
	}
	sb.WriteString(":\n")

	// Sort keys for deterministic output.
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		sb.WriteString(fmt.Sprintf("  %s = %s\n", k, vars[k]))
	}
	return strings.TrimRight(sb.String(), "\n")
}
