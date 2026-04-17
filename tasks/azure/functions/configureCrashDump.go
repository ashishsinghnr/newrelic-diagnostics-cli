package functions

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	log "github.com/newrelic/newrelic-diagnostics-cli/logger"
	"github.com/newrelic/newrelic-diagnostics-cli/tasks"
)

// crashDumpSettings are the Azure App Settings applied to enable full .NET crash dumps.
var crashDumpSettings = map[string]string{
	"DOTNET_DbgEnableMiniDump": "1",
	"DOTNET_DbgMiniDumpType":   "4",
	"DOTNET_DbgMiniDumpName":   "/home/LogFiles/dumps/coredump.%p.%t",
}

// javaCrashDumpSettings are the Azure App Settings applied to enable JVM OOM heap dumps.
var javaCrashDumpSettings = map[string]string{
	"JAVA_TOOL_OPTIONS": "-XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/home/LogFiles/dumps/ -XX:+ExitOnOutOfMemoryError",
}

// ConfigureCrashDumpResult holds the settings that were applied.
type ConfigureCrashDumpResult struct {
	FunctionAppName string
	ResourceGroup   string
	AppliedSettings map[string]string
}

// AzureFunctionsConfigureCrashDump interactively prompts the user to configure
// .NET crash dump collection on the Azure Function App by setting the required
// app settings via the az CLI.
type AzureFunctionsConfigureCrashDump struct {
	// cmdRunner is injectable for tests.
	cmdRunner func(name string, args ...string) ([]byte, error)
}

// Identifier returns the task identifier.
func (t AzureFunctionsConfigureCrashDump) Identifier() tasks.Identifier {
	return tasks.IdentifierFromString("Azure/Functions/ConfigureCrashDump")
}

// Explain returns the help text for this task.
func (t AzureFunctionsConfigureCrashDump) Explain() string {
	return "Interactively configure crash dump / OOM heap dump collection on an Azure Function App"
}

// Dependencies ensures this task runs after the live memory dump prompt.
func (t AzureFunctionsConfigureCrashDump) Dependencies() []string {
	return []string{
		"Azure/Functions/CollectLiveMemoryDump",
		taskDownloadSiteDump,
		taskDetectRuntime,
	}
}

// Execute prompts the user to configure crash dump collection. If confirmed,
// it calls 'az functionapp config appsettings set' to apply the settings.
func (t AzureFunctionsConfigureCrashDump) Execute(options tasks.Options, upstream map[string]tasks.Result) tasks.Result {
	funcName, resourceGroup := resolveFunctionTarget(options, upstream)
	if funcName == "" || resourceGroup == "" {
		return tasks.Result{
			Status:  tasks.None,
			Summary: "Skipped: functionName and resourceGroup options are required",
		}
	}

	runtime, _ := upstream[taskDetectRuntime].Payload.(string)

	var settings map[string]string
	var promptMsg, docURL string
	if IsDotnetRuntime(runtime) {
		settings = crashDumpSettings
		promptMsg = "Do you want to configure crash dump collection for crash time?"
		docURL = "https://docs.newrelic.com/docs/apm/agents/net-agent/azure-installation/install-net-agent-azure-web-apps/"
	} else if IsJavaRuntime(runtime) {
		settings = javaCrashDumpSettings
		promptMsg = "Do you want to configure JVM OOM heap dump collection?"
		docURL = "https://docs.newrelic.com/docs/apm/agents/java-agent/configuration/java-agent-configuration-config-file/"
	} else {
		return tasks.Result{
			Status:  tasks.None,
			Summary: fmt.Sprintf("Runtime is %q; crash-time dump configuration via environment variables is not available for this runtime", runtime),
		}
	}

	time.Sleep(promptFlushDelay * time.Millisecond)
	if !tasks.PromptUser(promptMsg, options) {
		return tasks.Result{
			Status:  tasks.None,
			Summary: "Skipped by user",
		}
	}

	runner := t.cmdRunner
	if runner == nil {
		runner = defaultCmdRunner
	}

	if err := applyAppSettings(runner, funcName, resourceGroup, settings); err != nil {
		log.Debug("Azure/Functions/ConfigureCrashDump: az CLI error: " + err.Error())
		return tasks.Result{
			Status:  tasks.Error,
			Summary: fmt.Sprintf("Failed to apply crash dump settings: %s", err.Error()),
			URL:     docURL,
		}
	}

	applied := make(map[string]string, len(settings))
	for k, v := range settings {
		applied[k] = v
	}
	result := &ConfigureCrashDumpResult{
		FunctionAppName: funcName,
		ResourceGroup:   resourceGroup,
		AppliedSettings: applied,
	}

	return tasks.Result{
		Status:  tasks.Success,
		Summary: formatConfigureSummary(funcName, settings),
		Payload: result,
		URL:     docURL,
	}
}

// applyAppSettings runs az functionapp config appsettings set to apply key=value pairs.
func applyAppSettings(runner func(string, ...string) ([]byte, error), funcName, resourceGroup string, settings map[string]string) error {
	args := []string{
		"functionapp", "config", "appsettings", "set",
		"--name", funcName,
		"--resource-group", resourceGroup,
		"--settings",
	}

	for k, v := range settings {
		args = append(args, fmt.Sprintf("%s=%s", k, v))
	}

	out, err := runner("az", args...)
	if err != nil {
		return fmt.Errorf("az functionapp config appsettings set failed: %w", err)
	}

	// az returns the full settings list as JSON on success. Non-JSON output
	// means az printed an error message instead of the settings list.
	var result interface{}
	if jsonErr := json.Unmarshal(out, &result); jsonErr != nil {
		log.Debug("Azure/Functions/ConfigureCrashDump: unexpected az output: " + string(out))
		return fmt.Errorf("az returned unexpected output (settings may not have been applied): %s", string(out))
	}

	return nil
}

func formatConfigureSummary(funcName string, settings map[string]string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Crash dump collection configured on %q. Applied settings:\n", funcName))
	keys := make([]string, 0, len(settings))
	for k := range settings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		sb.WriteString(fmt.Sprintf("  %s = %s\n", k, settings[k]))
	}
	sb.WriteString("\nNote: Azure will restart the Function App to apply the new settings.")
	return strings.TrimRight(sb.String(), "\n")
}
