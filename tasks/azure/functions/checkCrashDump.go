package functions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	log "github.com/newrelic/newrelic-diagnostics-cli/logger"
	"github.com/newrelic/newrelic-diagnostics-cli/tasks"
)

// defaultDumpScanDir is the directory scanned for existing crash dump files.
const defaultDumpScanDir = "/home/LogFiles/dumps"

// dumpFilePatterns are glob patterns matched against the dump scan directory.
var dumpFilePatterns = []string{"coredump.*", "*.dmp", "core.*"}

// DumpFile holds metadata about a crash dump file found on the filesystem.
type DumpFile struct {
	Name    string
	Path    string
	SizeMB  float64
}

// CrashDumpConfig holds the parsed crash dump configuration.
type CrashDumpConfig struct {
	Enabled        bool
	DumpType       string
	DumpPath       string
	UsingLegacy    bool
	RawSettings    map[string]string
	DumpFilesFound []DumpFile
}

var dumpTypeNames = map[string]string{
	"1": "Mini",
	"2": "Heap",
	"3": "Triage",
	"4": "Full",
}

// AzureFunctionsCheckCrashDumpConfig reports whether .NET crash dump collection
// is configured on the Azure Function App via environment variables, and also
// scans the local filesystem for existing dump files.
type AzureFunctionsCheckCrashDumpConfig struct {
	// dumpDirScanner can be overridden in tests to avoid real filesystem access.
	dumpDirScanner func(dir string, patterns []string) ([]DumpFile, error)
}

// Identifier returns the task identifier.
func (t AzureFunctionsCheckCrashDumpConfig) Identifier() tasks.Identifier {
	return tasks.IdentifierFromString("Azure/Functions/CheckCrashDumpConfig")
}

// Explain returns the help text for this task.
func (t AzureFunctionsCheckCrashDumpConfig) Explain() string {
	return "Check .NET crash dump collection configuration and scan for existing dump files in an Azure Function App"
}

// Dependencies returns the upstream tasks this task depends on.
func (t AzureFunctionsCheckCrashDumpConfig) Dependencies() []string {
	return []string{
		taskDetectFunctionApp,
		taskDetectRuntime,
		"Base/Env/CollectEnvVars",
		taskFetchAppSettings,
	}
}

// Execute reads crash dump env vars, reports configuration status, and scans
// the filesystem for any existing dump files.
func (t AzureFunctionsCheckCrashDumpConfig) Execute(options tasks.Options, upstream map[string]tasks.Result) tasks.Result {
	envVars, ok := resolveEnvVars(upstream)
	if !ok {
		log.Debug("Azure/Functions/CheckCrashDumpConfig: no env vars available from remote or local")
		return tasks.Result{
			Status:  tasks.None,
			Summary: "Not running in an Azure Function App and no remote settings available; this task did not run",
		}
	}

	runtime, _ := upstream[taskDetectRuntime].Payload.(string)

	scanner := t.dumpDirScanner
	if scanner == nil {
		scanner = scanDumpDir
	}

	if IsDotnetRuntime(runtime) {
		cfg := parseCrashDumpConfig(envVars)
		dumps, err := scanner(defaultDumpScanDir, dumpFilePatterns)
		if err != nil {
			log.Debug("Azure/Functions/CheckCrashDumpConfig: dump dir scan error: " + err.Error())
		}
		cfg.DumpFilesFound = dumps
		status := tasks.Info
		if !cfg.Enabled {
			status = tasks.Warning
		}
		return tasks.Result{
			Status:  status,
			Summary: formatCrashDumpSummary(cfg, runtime),
			Payload: cfg,
			URL:     "https://docs.newrelic.com/docs/apm/agents/net-agent/azure-installation/install-net-agent-azure-web-apps/",
		}
	}

	if IsJavaRuntime(runtime) {
		cfg := parseJavaHeapDumpConfig(envVars)
		dumps, err := scanner(defaultDumpScanDir, dumpFilePatterns)
		if err != nil {
			log.Debug("Azure/Functions/CheckCrashDumpConfig: dump dir scan error: " + err.Error())
		}
		cfg.DumpFilesFound = dumps
		status := tasks.Info
		if !cfg.Enabled {
			status = tasks.Warning
		}
		return tasks.Result{
			Status:  status,
			Summary: formatJavaHeapDumpSummary(cfg, runtime),
			Payload: cfg,
			URL:     "https://docs.newrelic.com/docs/apm/agents/java-agent/configuration/java-agent-configuration-config-file/",
		}
	}

	// Node.js and Python have no env-var-based crash-time dump mechanism.
	return tasks.Result{
		Status:  tasks.None,
		Summary: fmt.Sprintf("Runtime is %q; crash-time dump configuration via environment variables is not available for this runtime", runtime),
	}
}

// parseCrashDumpConfig reads crash dump configuration from an env var map.
func parseCrashDumpConfig(envVars map[string]string) *CrashDumpConfig {
	cfg := &CrashDumpConfig{
		RawSettings: make(map[string]string),
	}

	dotnetEnabled := envVars["DOTNET_DbgEnableMiniDump"]
	legacyEnabled := envVars["COMPlus_DbgEnableMiniDump"]

	cfg.Enabled = dotnetEnabled == "1" || legacyEnabled == "1"
	cfg.UsingLegacy = legacyEnabled == "1" && dotnetEnabled != "1"

	dumpTypeStr := firstNonEmpty(
		envVars["DOTNET_DbgMiniDumpType"],
		envVars["COMPlus_DbgMiniDumpType"],
	)
	if name, ok := dumpTypeNames[dumpTypeStr]; ok {
		cfg.DumpType = name
	} else if dumpTypeStr != "" {
		cfg.DumpType = dumpTypeStr
	}

	cfg.DumpPath = firstNonEmpty(
		envVars["DOTNET_DbgMiniDumpName"],
		envVars["COMPlus_DbgMiniDumpName"],
	)

	for _, key := range []string{
		"DOTNET_DbgEnableMiniDump", "DOTNET_DbgMiniDumpType", "DOTNET_DbgMiniDumpName",
		"COMPlus_DbgEnableMiniDump", "COMPlus_DbgMiniDumpType", "COMPlus_DbgMiniDumpName",
	} {
		if v, ok := envVars[key]; ok {
			cfg.RawSettings[key] = v
		}
	}

	return cfg
}

// scanDumpDir looks for crash dump files matching the given patterns under dir.
func scanDumpDir(dir string, patterns []string) ([]DumpFile, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, nil // directory doesn't exist — not an error, just no dumps
	}

	var found []DumpFile
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			continue
		}
		for _, path := range matches {
			info, err := os.Stat(path)
			if err != nil {
				log.Debug("Azure/Functions/CheckCrashDumpConfig: skipping " + path + ": " + err.Error())
				continue
			}
			found = append(found, DumpFile{
				Name:   filepath.Base(path),
				Path:   path,
				SizeMB: float64(info.Size()) / 1024 / 1024,
			})
		}
	}
	return found, nil
}

// parseJavaHeapDumpConfig reads JVM OOM heap dump configuration from env vars.
// It checks JAVA_TOOL_OPTIONS first (safe to set independently), then JAVA_OPTS
// and WEBSITE_JAVA_OPTS.
func parseJavaHeapDumpConfig(envVars map[string]string) *CrashDumpConfig {
	cfg := &CrashDumpConfig{
		RawSettings: make(map[string]string),
		DumpType:    "JVM Heap",
	}

	javaOpts := firstNonEmpty(
		envVars["JAVA_TOOL_OPTIONS"],
		envVars["JAVA_OPTS"],
		envVars["WEBSITE_JAVA_OPTS"],
	)

	if javaOpts != "" {
		cfg.Enabled = strings.Contains(javaOpts, "-XX:+HeapDumpOnOutOfMemoryError")
		for _, field := range strings.Fields(javaOpts) {
			if strings.HasPrefix(field, "-XX:HeapDumpPath=") {
				cfg.DumpPath = strings.TrimPrefix(field, "-XX:HeapDumpPath=")
			}
		}
	}

	for _, key := range []string{"JAVA_TOOL_OPTIONS", "JAVA_OPTS", "WEBSITE_JAVA_OPTS"} {
		if v, ok := envVars[key]; ok {
			cfg.RawSettings[key] = v
		}
	}

	return cfg
}

func formatJavaHeapDumpSummary(cfg *CrashDumpConfig, runtime string) string {
	var sb strings.Builder

	if !cfg.Enabled {
		sb.WriteString(fmt.Sprintf(
			"JVM heap dump on OutOfMemoryError is NOT configured for runtime %q.\n"+
				"To enable it, add to JAVA_TOOL_OPTIONS (or JAVA_OPTS):\n"+
				"  -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/home/LogFiles/dumps/ -XX:+ExitOnOutOfMemoryError",
			runtime,
		))
	} else {
		sb.WriteString(fmt.Sprintf("JVM heap dump on OutOfMemoryError is configured for runtime %q\n", runtime))
		sb.WriteString("  Enabled : true\n")
		if cfg.DumpPath != "" {
			sb.WriteString(fmt.Sprintf("  Path    : %s\n", cfg.DumpPath))
		}
	}

	if len(cfg.DumpFilesFound) > 0 {
		sb.WriteString(fmt.Sprintf("\n  Found %d dump file(s) on disk:\n", len(cfg.DumpFilesFound)))
		for _, f := range cfg.DumpFilesFound {
			sb.WriteString(fmt.Sprintf("    %s (%.2f MB)\n", f.Name, f.SizeMB))
		}
	}

	return strings.TrimRight(sb.String(), "\n")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func formatCrashDumpSummary(cfg *CrashDumpConfig, runtime string) string {
	var sb strings.Builder

	if !cfg.Enabled {
		sb.WriteString(fmt.Sprintf(
			"Crash dump collection is NOT configured for runtime %q.\n"+
				"To enable it, set DOTNET_DbgEnableMiniDump=1, DOTNET_DbgMiniDumpType=4 (Full),\n"+
				"and DOTNET_DbgMiniDumpName=/home/LogFiles/dumps/coredump.%%p.%%t as Azure App Settings.",
			runtime,
		))
	} else {
		sb.WriteString(fmt.Sprintf("Crash dump collection is configured for runtime %q\n", runtime))
		sb.WriteString("  Enabled : true\n")
		if cfg.UsingLegacy {
			sb.WriteString("  Vars    : COMPlus_ (legacy .NET <5)\n")
		} else {
			sb.WriteString("  Vars    : DOTNET_ (modern)\n")
		}
		if cfg.DumpType != "" {
			sb.WriteString(fmt.Sprintf("  Type    : %s\n", cfg.DumpType))
		}
		if cfg.DumpPath != "" {
			sb.WriteString(fmt.Sprintf("  Path    : %s\n", cfg.DumpPath))
		}
	}

	if len(cfg.DumpFilesFound) > 0 {
		sb.WriteString(fmt.Sprintf("\n  Found %d crash dump file(s) on disk:\n", len(cfg.DumpFilesFound)))
		for _, f := range cfg.DumpFilesFound {
			sb.WriteString(fmt.Sprintf("    %s (%.2f MB)\n", f.Name, f.SizeMB))
		}
	}

	return strings.TrimRight(sb.String(), "\n")
}
