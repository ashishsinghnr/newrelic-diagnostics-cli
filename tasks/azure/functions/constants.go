package functions

// Task identifier constants shared across the Azure Functions diagnostics package.
const (
	taskFetchAppSettings      = "Azure/Functions/FetchAppSettings"
	taskDownloadSiteDump      = "Azure/Functions/DownloadSiteDump"
	taskDetectFunctionApp     = "Azure/Functions/DetectFunctionApp"
	taskCollectProcessDetails = "Azure/Functions/CollectProcessDetails"
	taskDetectRuntime         = "Azure/Functions/DetectRuntime"
)

// promptFlushDelay is a short pause before interactive prompts to allow the
// async results channel to drain and print the previous task result before
// the prompt text appears on screen.
const promptFlushDelay = 300

// maxGeneralErrors is the maximum number of non-NR general error log entries
// collected during log analysis to avoid overwhelming the output.
const maxGeneralErrors = 50

// maxReportedEntries is the maximum number of NR errors/warnings shown in the
// CollectReport summary output per category.
const maxReportedEntries = 10
