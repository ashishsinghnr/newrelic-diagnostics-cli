package functions

import (
	"fmt"
	"regexp"
	"strings"

	log "github.com/newrelic/newrelic-diagnostics-cli/logger"
	"github.com/newrelic/newrelic-diagnostics-cli/tasks"
)

var licenseKeyFormat = regexp.MustCompile(`^[a-zA-Z0-9]{40}$`)

// newRelicDotnetProfilerGUID is the well-known CORECLR_PROFILER GUID for the NR .NET agent.
const newRelicDotnetProfilerGUID = "{36032161-FFC0-4B61-B559-F6C5D41BAE5A}"

// AzureFunctionsValidateAgentConfig validates that the New Relic agent environment
// variables are correctly configured inside an Azure Function App.
type AzureFunctionsValidateAgentConfig struct{}

// Identifier returns the task identifier.
func (t AzureFunctionsValidateAgentConfig) Identifier() tasks.Identifier {
	return tasks.IdentifierFromString("Azure/Functions/ValidateAgentConfig")
}

// Explain returns the help text for this task.
func (t AzureFunctionsValidateAgentConfig) Explain() string {
	return "Validate New Relic agent configuration environment variables in an Azure Function App"
}

// Dependencies returns the upstream tasks this task depends on.
func (t AzureFunctionsValidateAgentConfig) Dependencies() []string {
	return []string{
		taskDetectFunctionApp,
		"Azure/Functions/DetectRuntime",
		"Base/Env/CollectEnvVars",
		taskFetchAppSettings,
	}
}

// Execute validates the NR agent config env vars and returns Success/Warning/Failure.
func (t AzureFunctionsValidateAgentConfig) Execute(options tasks.Options, upstream map[string]tasks.Result) tasks.Result {
	envVars, ok := resolveEnvVars(upstream)
	if !ok {
		log.Debug("Azure/Functions/ValidateAgentConfig: no env vars available from remote or local")
		return tasks.Result{
			Status:  tasks.None,
			Summary: "Not running in an Azure Function App and no remote settings available; this task did not run",
		}
	}

	runtime, _ := upstream["Azure/Functions/DetectRuntime"].Payload.(string)

	failures, warnings, summaryLines := collectValidationResults(envVars, runtime)

	if len(failures) > 0 {
		return tasks.Result{
			Status:  tasks.Failure,
			Summary: buildSummary("New Relic agent configuration issues found", failures, warnings, summaryLines),
			URL:     "https://docs.newrelic.com/docs/serverless-function-monitoring/azure-functions/install/",
		}
	}
	if len(warnings) > 0 {
		return tasks.Result{
			Status:  tasks.Warning,
			Summary: buildSummary("New Relic agent configuration has warnings", nil, warnings, summaryLines),
			URL:     "https://docs.newrelic.com/docs/serverless-function-monitoring/azure-functions/install/",
		}
	}

	return tasks.Result{
		Status:  tasks.Success,
		Summary: buildSummary("New Relic agent configuration looks correct", nil, nil, summaryLines),
	}
}

// collectValidationResults runs all validation checks and returns failures, warnings, and summary lines.
func collectValidationResults(envVars map[string]string, runtime string) (failures, warnings, summaryLines []string) {
	f, s := validateLicenseKey(envVars)
	failures = append(failures, f...)
	summaryLines = append(summaryLines, s...)

	var w []string
	w, s = validateAppName(envVars)
	warnings = append(warnings, w...)
	summaryLines = append(summaryLines, s...)

	if IsDotnetRuntime(runtime) {
		f, w, s := validateDotnetProfilerVars(envVars)
		failures = append(failures, f...)
		warnings = append(warnings, w...)
		summaryLines = append(summaryLines, s...)
	}

	if IsJavaRuntime(runtime) {
		w, s := validateJavaAgentVars(envVars)
		warnings = append(warnings, w...)
		summaryLines = append(summaryLines, s...)
	}

	if IsNodeRuntime(runtime) {
		w, s := validateNodeAgentVars(envVars)
		warnings = append(warnings, w...)
		summaryLines = append(summaryLines, s...)
	}

	if IsPythonRuntime(runtime) {
		w, s := validatePythonAgentVars(envVars)
		warnings = append(warnings, w...)
		summaryLines = append(summaryLines, s...)
	}

	if envVars["APPLICATIONINSIGHTS_CONNECTION_STRING"] != "" {
		warnings = append(warnings, "APPLICATIONINSIGHTS_CONNECTION_STRING is set alongside New Relic; verify that distributed tracing and sampling configurations do not conflict")
	}

	return failures, warnings, summaryLines
}

func validateLicenseKey(envVars map[string]string) (failures, summaryLines []string) {
	licenseKey := strings.TrimSpace(envVars["NEW_RELIC_LICENSE_KEY"])
	if licenseKey == "" {
		failures = append(failures, "NEW_RELIC_LICENSE_KEY is not set")
	} else if !licenseKeyFormat.MatchString(licenseKey) {
		failures = append(failures, "NEW_RELIC_LICENSE_KEY does not match the expected 40-character alphanumeric format")
	} else {
		summaryLines = append(summaryLines, "NEW_RELIC_LICENSE_KEY: present and correctly formatted")
	}
	return failures, summaryLines
}

func validateAppName(envVars map[string]string) (warnings, summaryLines []string) {
	appName := strings.TrimSpace(envVars["NEW_RELIC_APP_NAME"])
	if appName == "" {
		warnings = append(warnings, "NEW_RELIC_APP_NAME is not set; the agent will use a default name which may make it hard to locate in the New Relic UI")
	} else {
		summaryLines = append(summaryLines, fmt.Sprintf("NEW_RELIC_APP_NAME: %q", appName))
	}
	return warnings, summaryLines
}

func validateDotnetProfilerVars(envVars map[string]string) (failures, warnings, summaryLines []string) {
	if envVars["CORECLR_ENABLE_PROFILING"] != "1" {
		failures = append(failures, fmt.Sprintf(
			"CORECLR_ENABLE_PROFILING is %q; must be \"1\" for the .NET agent to attach",
			envVars["CORECLR_ENABLE_PROFILING"],
		))
	} else {
		summaryLines = append(summaryLines, "CORECLR_ENABLE_PROFILING=1")
	}

	profiler := envVars["CORECLR_PROFILER"]
	if profiler == "" {
		failures = append(failures, "CORECLR_PROFILER is not set; it must be set to the New Relic profiler GUID "+newRelicDotnetProfilerGUID)
	} else if !strings.EqualFold(profiler, newRelicDotnetProfilerGUID) {
		warnings = append(warnings, fmt.Sprintf("CORECLR_PROFILER is %q; expected New Relic profiler GUID %s", profiler, newRelicDotnetProfilerGUID))
	} else {
		summaryLines = append(summaryLines, "CORECLR_PROFILER: correct New Relic GUID")
	}

	if nrHome := envVars["CORECLR_NEWRELIC_HOME"]; nrHome == "" {
		failures = append(failures, "CORECLR_NEWRELIC_HOME is not set; it must point to the New Relic agent directory (e.g. /home/site/wwwroot/newrelic)")
	} else {
		summaryLines = append(summaryLines, fmt.Sprintf("CORECLR_NEWRELIC_HOME: %q", nrHome))
	}

	profilerPath := firstNonEmpty(envVars["CORECLR_PROFILER_PATH"], envVars["CORECLR_PROFILER_PATH_64"])
	if profilerPath == "" {
		failures = append(failures, "CORECLR_PROFILER_PATH (or CORECLR_PROFILER_PATH_64) is not set; the agent cannot load without a profiler path")
	} else {
		summaryLines = append(summaryLines, fmt.Sprintf("CORECLR_PROFILER_PATH: %q", profilerPath))
	}

	return failures, warnings, summaryLines
}

func validateJavaAgentVars(envVars map[string]string) (warnings, summaryLines []string) {
	javaOpts := firstNonEmpty(
		envVars["JAVA_OPTS"],
		envVars["WEBSITE_JAVA_OPTS"],
		envVars["JAVA_TOOL_OPTIONS"],
	)
	key := "JAVA_OPTS"
	if envVars["WEBSITE_JAVA_OPTS"] != "" {
		key = "WEBSITE_JAVA_OPTS"
	} else if envVars["JAVA_OPTS"] == "" && envVars["JAVA_TOOL_OPTIONS"] != "" {
		key = "JAVA_TOOL_OPTIONS"
	}

	if javaOpts == "" {
		warnings = append(warnings, "JAVA_OPTS (or WEBSITE_JAVA_OPTS) is not set; the New Relic Java agent requires -javaagent:/path/to/newrelic.jar (e.g. JAVA_OPTS=-javaagent:/home/site/wwwroot/newrelic/newrelic.jar)")
	} else {
		lower := strings.ToLower(javaOpts)
		if !strings.Contains(lower, "-javaagent:") {
			warnings = append(warnings, fmt.Sprintf("%s=%q does not contain a -javaagent: entry; the New Relic Java agent will not attach", key, javaOpts))
		} else if !strings.Contains(lower, "newrelic") {
			warnings = append(warnings, fmt.Sprintf("%s=%q has -javaagent: but does not reference newrelic.jar; verify the path is correct", key, javaOpts))
		} else {
			summaryLines = append(summaryLines, fmt.Sprintf("%s: contains -javaagent referencing newrelic.jar", key))
		}
	}
	return warnings, summaryLines
}

func validateNodeAgentVars(envVars map[string]string) (warnings, summaryLines []string) {
	if strings.ToLower(strings.TrimSpace(envVars["NEW_RELIC_NO_CONFIG_FILE"])) != "true" {
		warnings = append(warnings, "NEW_RELIC_NO_CONFIG_FILE is not set to \"true\"; without this the New Relic Node.js agent attempts to load a config file that does not exist in Azure Functions")
	} else {
		summaryLines = append(summaryLines, "NEW_RELIC_NO_CONFIG_FILE=true")
	}
	return warnings, summaryLines
}

func validatePythonAgentVars(envVars map[string]string) (warnings, summaryLines []string) {
	if strings.TrimSpace(envVars["PYTHONFAULTHANDLER"]) != "1" {
		warnings = append(warnings, "PYTHONFAULTHANDLER is not set to \"1\"; enabling it improves crash diagnostics by printing a Python traceback on fatal signals")
	} else {
		summaryLines = append(summaryLines, "PYTHONFAULTHANDLER=1")
	}
	return warnings, summaryLines
}

func buildSummary(header string, failures, warnings, info []string) string {
	var sb strings.Builder
	sb.WriteString(header + "\n")
	for _, f := range failures {
		sb.WriteString("  [FAILURE] " + f + "\n")
	}
	for _, w := range warnings {
		sb.WriteString("  [WARNING] " + w + "\n")
	}
	for _, i := range info {
		sb.WriteString("  [OK] " + i + "\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}
