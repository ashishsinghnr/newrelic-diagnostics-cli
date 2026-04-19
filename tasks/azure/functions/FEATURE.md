# Azure Functions Diagnostic Suite

## Overview

Added a new `azure-functions` diagnostic suite to `nrdiag` that validates New Relic agent configuration, crash dump setup, and collects live diagnostics from Azure Function Apps — across all supported runtimes: **.NET, Java, Node.js, and Python**.

Previously, `nrdiag` had no Azure Functions support. Engineers had to manually inspect app settings, check logs, and configure crash dumps — a slow, error-prone process during incidents.

---

## What Was Built

### 12 New Diagnostic Tasks (`tasks/azure/functions/`)

| Task | Purpose |
|------|---------|
| `DetectFunctionApp` | Confirms the target is an Azure Function App via `FUNCTIONS_WORKER_RUNTIME` |
| `FetchAppSettings` | Fetches app settings remotely via `az` CLI (for local machine usage) |
| `DetectRuntime` | Identifies runtime: `dotnet`, `dotnet-isolated`, `java`, `node`, `python` |
| `ValidateAgentConfig` | Validates NR agent env vars (license key, app name, runtime-specific vars) |
| `CheckCrashDumpConfig` | Reports crash dump / OOM heap dump configuration status |
| `AgentInfo` | Collects NR agent version and active configuration |
| `AnalyzeLogs` | Scans Function App logs for NR errors and warnings |
| `DownloadSiteDump` | Downloads Kudu site dump ZIP (logs, config, wwwroot snapshot) |
| `CollectReport` | Produces a structured diagnostic report |
| `CollectProcessDetails` | Collects running process properties via Kudu API |
| `CollectLiveMemoryDump` | Streams a live full memory dump via Kudu (interactive) |
| `ConfigureCrashDump` | Applies crash dump app settings via `az` CLI (interactive) |

### Supporting Changes

| File | Change |
|------|--------|
| `registration/registerTasks.go` | Registered all 13 tasks; removed a duplicate `pythonEnv` registration |
| `suites/suiteDefinitions.go` | Added `azure-functions` suite with display name `"Azure Functions Agent"` |

---

## Runtime-Aware Behaviour

Each task branches on the detected runtime so only relevant checks run:

| Runtime | `ValidateAgentConfig` checks | `CheckCrashDumpConfig` checks |
|---------|------------------------------|-------------------------------|
| `.NET` / `dotnet-isolated` | `CORECLR_ENABLE_PROFILING`, `CORECLR_PROFILER`, `CORECLR_NEWRELIC_HOME`, `CORECLR_PROFILER_PATH` | `DOTNET_DbgEnableMiniDump`, `DOTNET_DbgMiniDumpType`, `DOTNET_DbgMiniDumpName` |
| `java` | `JAVA_OPTS` / `JAVA_TOOL_OPTIONS` contains `-javaagent:newrelic.jar` | `JAVA_TOOL_OPTIONS` contains `-XX:+HeapDumpOnOutOfMemoryError` |
| `node` | `NEW_RELIC_NO_CONFIG_FILE=true` | Returns `None` (no env-var crash dump mechanism) |
| `python` | `PYTHONFAULTHANDLER=1` | Returns `None` (no env-var crash dump mechanism) |

---

## Execution Flow

### Phase 1 — Always runs automatically
1. `FetchAppSettings` / `DetectFunctionApp` — confirm Azure Function App
2. `DetectRuntime` — identify runtime
3. `ValidateAgentConfig` — validate NR agent env vars
4. `CheckCrashDumpConfig` — check crash dump / OOM heap dump setup
5. `AgentInfo` — gather agent version and config
6. `AnalyzeLogs` — scan logs for NR errors
7. `CollectReport` — produce structured summary

### Phase 2 — Optional, requires `-override` flags
Downloads the Kudu site dump ZIP (logs, config, wwwroot snapshot):
```
-override "Azure/Functions/DownloadSiteDump.functionName=<name>"
-override "Azure/Functions/DownloadSiteDump.resourceGroup=<rg>"
```

### Phase 3 — Optional, interactive prompts at runtime
- Collect process property details (`CollectProcessDetails`)
- Collect a live full memory dump (`CollectLiveMemoryDump`)
- Configure crash dump / OOM heap dump app settings (`ConfigureCrashDump`)

---

## Two Operating Modes

### Mode 1 — Running inside the Azure Function App container
No flags needed. Azure injects `WEBSITE_SITE_NAME` and `WEBSITE_RESOURCE_GROUP` automatically.

```bash
./nrdiag -suites azure-functions
```

### Mode 2 — Running from a developer's local machine
Requires `-override` so `FetchAppSettings` can call `az` CLI to pull remote settings.

```bash
./nrdiag -suites azure-functions \
  -override "Azure/Functions/DownloadSiteDump.functionName=<function-app-name>" \
  -override "Azure/Functions/DownloadSiteDump.resourceGroup=<resource-group>"
```

Add `-y` to skip all interactive prompts (non-interactive / CI use).

---

## Test Coverage

- **197 unit tests** added across 12 test files, covering all tasks and helper functions
- Tests use Ginkgo/Gomega BDD framework consistent with the rest of the codebase
- Filesystem and `az` CLI interactions are fully mocked — no real Azure access required to run tests

---

## Validated Against Real Environment

Tested against a live Azure Function App (`dotnet-isolated` runtime):
- Fetched 21 app settings remotely via `az` CLI
- Detected runtime correctly
- Validated NR agent config → `Success`
- Confirmed crash dump collection already configured → `Info`
- Collected 10 NR agent settings
- Downloaded site dump and generated diagnostic report
