package functions

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	log "github.com/newrelic/newrelic-diagnostics-cli/logger"
	"github.com/newrelic/newrelic-diagnostics-cli/tasks"
)

// azureFunctionAppNameFormat enforces Azure's documented constraints for
// function-app names: 2–60 characters, ASCII letters/digits/hyphen, and not
// starting or ending with a hyphen. Validating at the URL boundary defends
// against injection of arbitrary hostnames into the SCM endpoint, even though
// url.PathEscape covers most cases incidentally.
var azureFunctionAppNameFormat = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,58}[a-zA-Z0-9])?$`)

// azureResourceGroupFormat enforces Azure's documented constraints for resource
// group names: 1–90 characters of letters, digits, and '_.()-', not ending in a
// period. Crucially it forbids a leading hyphen, so a value like "--debug" can
// never reach the az CLI where its argument parser would treat it as a flag
// rather than the value of the preceding option (argument injection).
var azureResourceGroupFormat = regexp.MustCompile(`^[a-zA-Z0-9_()]$|^[a-zA-Z0-9_.()][a-zA-Z0-9_.()-]{0,88}[a-zA-Z0-9_()-]$`)

// validateAzureTarget rejects functionName/resourceGroup values that do not
// satisfy Azure's naming rules before they are passed to the az CLI. Both
// allowlists exclude a leading '-', which prevents a crafted value from being
// interpreted as a CLI flag by az's argument parser. This is the single
// validation boundary for every task that shells out to az.
func validateAzureTarget(funcName, resourceGroup string) error {
	if !azureFunctionAppNameFormat.MatchString(funcName) {
		return fmt.Errorf("invalid Azure Function App name %q: must be 2–60 characters of [a-zA-Z0-9-] and may not start or end with '-'", funcName)
	}
	if !azureResourceGroupFormat.MatchString(resourceGroup) {
		return fmt.Errorf("invalid Azure resource group name %q: must be 1–90 characters of [a-zA-Z0-9_.()-], may not start with '-', and may not end with '.'", resourceGroup)
	}
	return nil
}

// buildScmURL returns the Kudu SCM base URL for the given function app name.
// It returns an error if funcName does not satisfy Azure's hostname rules.
func buildScmURL(funcName string) (string, error) {
	if !azureFunctionAppNameFormat.MatchString(funcName) {
		return "", fmt.Errorf("invalid Azure Function App name %q: must be 2–60 characters of [a-zA-Z0-9-] and may not start or end with '-'", funcName)
	}
	return fmt.Sprintf("https://%s.scm.azurewebsites.net", funcName), nil
}

// azureAppSetting is a single entry from az functionapp config appsettings list output.
type azureAppSetting struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// sensitiveKeySubstrings lists case-insensitive substrings in an app-setting
// name that indicate the value is a secret. Azure Function Apps store storage
// account connection strings, registry passwords, and instrumentation keys
// under names that do not match the New Relic sensitiveKeySuffixes, so
// app-setting obfuscation also checks for these substrings.
var sensitiveKeySubstrings = []string{
	"CONNECTIONSTRING", "CONNECTION_STRING", "ACCOUNTKEY", "ACCOUNT_KEY",
	"ACCESSKEY", "ACCESS_KEY", "STORAGE", "SAS", "SECRET", "PASSWORD", "PWD",
	"TOKEN", "APIKEY", "API_KEY", "INSTRUMENTATIONKEY", "CLIENTSECRET",
	"CLIENT_SECRET", "CREDENTIAL",
}

// connectionStringValuePattern matches values shaped like Azure storage/SQL
// connection strings or shared-access signatures, so credentials stored under
// an innocuous key name (e.g. a custom "MyBackend" setting) are still masked.
var connectionStringValuePattern = regexp.MustCompile(`(?i)(AccountKey=|SharedAccessKey=|SharedAccessSignature=|DefaultEndpointsProtocol=|Password=|Pwd=|sig=[A-Za-z0-9%]{20,})`)

// maskAppSettingValue obfuscates an Azure app-setting value that is likely a
// secret. New Relic settings keep their existing masking (maskIfSensitive: a
// short prefix helps operators identify which NR credential is set). Non-New-
// Relic secrets — storage account keys, connection strings, registry passwords
// — are fully masked so no prefix of an unrelated secret ever leaks into the
// stored artifact.
func maskAppSettingValue(key, val string) string {
	if val == "" {
		return val
	}
	upper := strings.ToUpper(key)
	if strings.HasPrefix(upper, "NEW_RELIC_") || strings.HasPrefix(upper, "NEWRELIC_") {
		return maskIfSensitive(key, val)
	}
	// Non-New-Relic keys: fully mask if the name matches a New Relic suffix,
	// an Azure secret substring, or the value is shaped like a connection string.
	if maskIfSensitive(key, val) != val {
		return "****"
	}
	for _, sub := range sensitiveKeySubstrings {
		if strings.Contains(upper, sub) {
			return "****"
		}
	}
	if connectionStringValuePattern.MatchString(val) {
		return "****"
	}
	return val
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

	if err := validateAzureTarget(funcName, resourceGroup); err != nil {
		return tasks.Result{
			Status:  tasks.Error,
			Summary: err.Error(),
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

// ObfuscatePayload implements tasks.PayloadObfuscator. App settings are held
// unmasked in memory so downstream tasks (e.g. ValidateAgentConfig, which
// checks the real NEW_RELIC_LICENSE_KEY format) can consume them during the
// run. This method runs only at the artifact-write boundary, masking secret
// values — New Relic keys plus non-NR secrets such as storage account keys and
// connection strings — before they are serialized into nrdiag-output.json.
func (t AzureFunctionsFetchAppSettings) ObfuscatePayload(payload interface{}) interface{} {
	settings, ok := payload.(map[string]string)
	if !ok {
		return payload
	}
	masked := make(map[string]string, len(settings))
	for k, v := range settings {
		masked[k] = maskAppSettingValue(k, v)
	}
	return masked
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
	// --flag=value form binds each value to its flag so az's argument parser can
	// never interpret a value as a separate flag (defense-in-depth alongside
	// validateAzureTarget).
	out, err := runner("az", "functionapp", "config", "appsettings", "list",
		"--name="+funcName,
		"--resource-group="+resourceGroup,
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
