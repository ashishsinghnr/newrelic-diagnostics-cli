package functions

import (
	log "github.com/newrelic/newrelic-diagnostics-cli/logger"
	"github.com/newrelic/newrelic-diagnostics-cli/tasks"
)

// RegisterWith registers all Azure/Functions/* tasks with the provided registration function.
func RegisterWith(registrationFunc func(tasks.Task, bool)) {
	log.Debug("Registering Azure/Functions/*")

	registrationFunc(AzureFunctionsFetchAppSettings{}, false)
	registrationFunc(AzureFunctionsDetectFunctionApp{}, false)
	registrationFunc(AzureFunctionsDetectRuntime{}, false)
	registrationFunc(AzureFunctionsValidateAgentConfig{}, false)
	registrationFunc(AzureFunctionsCheckCrashDumpConfig{}, false)
	registrationFunc(AzureFunctionsAgentInfo{}, false)
	registrationFunc(AzureFunctionsAnalyzeLogs{}, false)
	registrationFunc(AzureFunctionsDownloadSiteDump{}, false)
	registrationFunc(AzureFunctionsCollectReport{}, false)
	registrationFunc(AzureFunctionsCollectLiveMemoryDump{}, false)
	registrationFunc(AzureFunctionsConfigureCrashDump{}, false)
	registrationFunc(AzureFunctionsCollectProcessDetails{}, false)
}
