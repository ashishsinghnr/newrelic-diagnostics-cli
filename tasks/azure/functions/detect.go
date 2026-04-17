package functions

import (
	log "github.com/newrelic/newrelic-diagnostics-cli/logger"
	"github.com/newrelic/newrelic-diagnostics-cli/tasks"
)

// azureFunctionsEnvVars lists env vars that are present in Azure Function Apps.
// FUNCTIONS_WORKER_RUNTIME is injected only in Function Apps (not plain App Service),
// making it the reliable discriminator.
var azureFunctionsEnvVars = []string{
	"FUNCTIONS_WORKER_RUNTIME",
	"FUNCTIONS_EXTENSION_VERSION",
	"WEBSITE_SITE_NAME",
	"WEBSITE_RESOURCE_GROUP",
}

// AzureFunctionsDetectFunctionApp detects whether nrdiag is running inside an
// Azure Function App by checking for Function-App-specific environment variables.
type AzureFunctionsDetectFunctionApp struct{}

// Identifier returns the task identifier.
func (t AzureFunctionsDetectFunctionApp) Identifier() tasks.Identifier {
	return tasks.IdentifierFromString("Azure/Functions/DetectFunctionApp")
}

// Explain returns the help text for this task.
func (t AzureFunctionsDetectFunctionApp) Explain() string {
	return "Detect if running inside an Azure Function App"
}

// Dependencies returns the upstream tasks this task depends on.
func (t AzureFunctionsDetectFunctionApp) Dependencies() []string {
	return []string{
		"Base/Env/CollectEnvVars",
		"Base/Env/DetectAzure",
	}
}

// Execute checks environment variables to determine if the current environment
// is an Azure Function App. It returns Info with a payload of the relevant env
// vars if detected, or None if not.
func (t AzureFunctionsDetectFunctionApp) Execute(options tasks.Options, upstream map[string]tasks.Result) tasks.Result {
	if upstream["Base/Env/CollectEnvVars"].Status == tasks.Warning {
		return tasks.Result{
			Status:  tasks.None,
			Summary: "Unable to gather environment variables; this task did not run",
		}
	}

	if upstream["Base/Env/DetectAzure"].Status != tasks.Info {
		return tasks.Result{
			Status:  tasks.None,
			Summary: "Not running in an Azure environment; this task did not run",
		}
	}

	envVars, ok := upstream["Base/Env/CollectEnvVars"].Payload.(map[string]string)
	if !ok {
		log.Debug("Azure/Functions/DetectFunctionApp: could not cast CollectEnvVars payload")
		envVars = map[string]string{}
	}

	// FUNCTIONS_WORKER_RUNTIME is the definitive Azure Functions signal.
	if _, present := envVars["FUNCTIONS_WORKER_RUNTIME"]; !present {
		return tasks.Result{
			Status:  tasks.None,
			Summary: "FUNCTIONS_WORKER_RUNTIME not set; not running in an Azure Function App",
		}
	}

	// Collect all relevant Function App env vars for downstream tasks.
	detected := make(map[string]string)
	for _, key := range azureFunctionsEnvVars {
		if val, present := envVars[key]; present {
			detected[key] = val
		}
	}

	return tasks.Result{
		Status:  tasks.Info,
		Summary: "Detected Azure Function App environment (FUNCTIONS_WORKER_RUNTIME=" + detected["FUNCTIONS_WORKER_RUNTIME"] + ")",
		Payload: detected,
	}
}
