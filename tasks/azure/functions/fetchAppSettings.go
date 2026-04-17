package functions

import (
	"encoding/json"
	"fmt"

	log "github.com/newrelic/newrelic-diagnostics-cli/logger"
	"github.com/newrelic/newrelic-diagnostics-cli/tasks"
)

// azureAppSetting is a single entry from az functionapp config appsettings list output.
type azureAppSetting struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// AzureFunctionsFetchAppSettings fetches the Azure Function App's application
// settings remotely via the az CLI. This allows validation tasks to run from
// a developer's local machine without needing to be inside the Azure container.
type AzureFunctionsFetchAppSettings struct {
	cmdRunner func(name string, args ...string) ([]byte, error)
}

// Identifier returns the task identifier.
func (t AzureFunctionsFetchAppSettings) Identifier() tasks.Identifier {
	return tasks.IdentifierFromString(taskFetchAppSettings)
}

// Explain returns the help text for this task.
func (t AzureFunctionsFetchAppSettings) Explain() string {
	return "Fetch Azure Function App application settings remotely via the az CLI"
}

// Dependencies runs after DownloadSiteDump so it can inherit functionName/resourceGroup.
func (t AzureFunctionsFetchAppSettings) Dependencies() []string {
	return []string{
		taskDownloadSiteDump,
	}
}

// Execute fetches app settings from Azure remotely. Only runs when functionName
// and resourceGroup are available (i.e. targeting a remote Azure environment).
func (t AzureFunctionsFetchAppSettings) Execute(options tasks.Options, upstream map[string]tasks.Result) tasks.Result {
	funcName, resourceGroup := resolveFunctionTarget(options, upstream)
	if funcName == "" || resourceGroup == "" {
		return tasks.Result{
			Status:  tasks.None,
			Summary: "Skipped: functionName and resourceGroup not provided; remote fetch not applicable",
		}
	}

	runner := t.cmdRunner
	if runner == nil {
		runner = defaultCmdRunner
	}

	settings, err := fetchSettingsFromAz(runner, funcName, resourceGroup)
	if err != nil {
		log.Debug("Azure/Functions/FetchAppSettings: az CLI error: " + err.Error())
		return tasks.Result{
			Status:  tasks.Error,
			Summary: fmt.Sprintf("Failed to fetch app settings via az CLI: %s", err.Error()),
			URL:     "https://learn.microsoft.com/en-us/cli/azure/functionapp/config/appsettings",
		}
	}

	return tasks.Result{
		Status:  tasks.Info,
		Summary: fmt.Sprintf("Fetched %d app settings from Azure Function App %q", len(settings), funcName),
		Payload: settings,
	}
}

// resolveFunctionTarget returns functionName and resourceGroup from options,
// falling back to the DownloadSiteDump payload if not explicitly set.
func resolveFunctionTarget(options tasks.Options, upstream map[string]tasks.Result) (string, string) {
	funcName := options.Options["functionName"]
	resourceGroup := options.Options["resourceGroup"]
	if funcName == "" || resourceGroup == "" {
		if siteDump, ok := upstream[taskDownloadSiteDump].Payload.(*SiteDumpResult); ok {
			if funcName == "" {
				funcName = siteDump.FunctionAppName
			}
			if resourceGroup == "" {
				resourceGroup = siteDump.ResourceGroup
			}
		}
	}
	return funcName, resourceGroup
}

// fetchSettingsFromAz calls the az CLI to list app settings and returns them as a map.
func fetchSettingsFromAz(runner func(string, ...string) ([]byte, error), funcName, resourceGroup string) (map[string]string, error) {
	out, err := runner("az", "functionapp", "config", "appsettings", "list",
		"--name", funcName,
		"--resource-group", resourceGroup,
		"-o", "json",
	)
	if err != nil {
		return nil, err
	}

	var raw []azureAppSetting
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse app settings response: %w", err)
	}

	settings := make(map[string]string, len(raw))
	for _, s := range raw {
		settings[s.Name] = s.Value
	}
	return settings, nil
}

// resolveEnvVars returns the env vars map to use for validation.
// Priority: remote FetchAppSettings (when targeting Azure) > local CollectEnvVars (inside container).
func resolveEnvVars(upstream map[string]tasks.Result) (map[string]string, bool) {
	if upstream[taskFetchAppSettings].Status == tasks.Info {
		if settings, ok := upstream[taskFetchAppSettings].Payload.(map[string]string); ok {
			return settings, true
		}
	}
	// Only fall back to local env vars when confirmed running inside Azure container.
	if upstream[taskDetectFunctionApp].Status == tasks.Info {
		if settings, ok := upstream["Base/Env/CollectEnvVars"].Payload.(map[string]string); ok {
			return settings, true
		}
	}
	return nil, false
}
