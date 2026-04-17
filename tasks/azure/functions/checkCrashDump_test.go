package functions

import (
	"github.com/newrelic/newrelic-diagnostics-cli/tasks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// mockDumpDirScanner returns a pre-set slice of DumpFiles for testing.
func mockDumpDirScanner(files []DumpFile) func(string, []string) ([]DumpFile, error) {
	return func(dir string, patterns []string) ([]DumpFile, error) {
		return files, nil
	}
}

const (
	upstreamDetectFunctionApp = "Azure/Functions/DetectFunctionApp"
	upstreamDetectRuntime     = "Azure/Functions/DetectRuntime"
	upstreamCollectEnvVars    = "Base/Env/CollectEnvVars"
	shouldReturnInfo          = "should return Info"
	shouldReturnFailure       = "should return Failure"
	runtimeDotnetIsolated     = "dotnet-isolated"
	defaultDumpPath           = "/home/LogFiles/dumps/coredump.%p.%t"
	testDumpFileName          = "coredump.1234.5678"
	jvmHeapDumpDir            = "/home/LogFiles/dumps/"
)

var _ = Describe("Azure/Functions/CheckCrashDumpConfig", func() {
	var p AzureFunctionsCheckCrashDumpConfig

	BeforeEach(func() {
		// Default to no dump files on disk so tests are isolated.
		p = AzureFunctionsCheckCrashDumpConfig{
			dumpDirScanner: mockDumpDirScanner(nil),
		}
	})

	Describe("Identifier()", func() {
		It("should return correct identifier", func() {
			Expect(p.Identifier()).To(Equal(tasks.Identifier{
				Category:    "Azure",
				Subcategory: "Functions",
				Name:        "CheckCrashDumpConfig",
			}))
		})
	})

	Describe("Dependencies()", func() {
		It("should include required upstream tasks", func() {
			Expect(p.Dependencies()).To(ConsistOf(
				upstreamDetectFunctionApp,
				upstreamDetectRuntime,
				upstreamCollectEnvVars,
				"Azure/Functions/FetchAppSettings",
			))
		})
	})

	Describe("Execute()", func() {
		var (
			result   tasks.Result
			options  tasks.Options
			upstream map[string]tasks.Result
		)

		JustBeforeEach(func() {
			result = p.Execute(options, upstream)
		})

		Context("when not in an Azure Function App", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.None},
					upstreamDetectRuntime:     {Status: tasks.None},
					upstreamCollectEnvVars:           {Status: tasks.Info, Payload: map[string]string{}},
				}
			})
			It("should return None", func() {
				Expect(result.Status).To(Equal(tasks.None))
			})
		})

		Context("when runtime is python (non-.NET)", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.Info},
					upstreamDetectRuntime:     {Status: tasks.Info, Payload: "python"},
					upstreamCollectEnvVars:           {Status: tasks.Info, Payload: map[string]string{}},
				}
			})
			It("should return None", func() {
				Expect(result.Status).To(Equal(tasks.None))
			})
		})

		Context("when runtime is dotnet-isolated and crash dumps are NOT configured", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.Info},
					upstreamDetectRuntime:     {Status: tasks.Info, Payload: runtimeDotnetIsolated},
					upstreamCollectEnvVars: {
						Status:  tasks.Info,
						Payload: map[string]string{},
					},
				}
			})
			It("should return Warning (crash dumps not configured)", func() {
				Expect(result.Status).To(Equal(tasks.Warning))
			})
			It("should say crash dumps are not configured", func() {
				Expect(result.Summary).To(ContainSubstring("NOT configured"))
			})
			It("should have a payload with Enabled=false", func() {
				cfg, ok := result.Payload.(*CrashDumpConfig)
				Expect(ok).To(BeTrue())
				Expect(cfg.Enabled).To(BeFalse())
			})
		})

		Context("when DOTNET_ crash dump vars are set", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.Info},
					upstreamDetectRuntime:     {Status: tasks.Info, Payload: runtimeDotnetIsolated},
					upstreamCollectEnvVars: {
						Status: tasks.Info,
						Payload: map[string]string{
							"DOTNET_DbgEnableMiniDump": "1",
							"DOTNET_DbgMiniDumpType":   "4",
							"DOTNET_DbgMiniDumpName":   defaultDumpPath,
						},
					},
				}
			})
			It(shouldReturnInfo, func() {
				Expect(result.Status).To(Equal(tasks.Info))
			})
			It("should report crash dumps as enabled with correct details", func() {
				cfg, ok := result.Payload.(*CrashDumpConfig)
				Expect(ok).To(BeTrue())
				Expect(cfg.Enabled).To(BeTrue())
				Expect(cfg.DumpType).To(Equal("Full"))
				Expect(cfg.DumpPath).To(Equal(defaultDumpPath))
				Expect(cfg.UsingLegacy).To(BeFalse())
			})
		})

		Context("when legacy COMPlus_ vars are set", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.Info},
					upstreamDetectRuntime:     {Status: tasks.Info, Payload: "dotnet"},
					upstreamCollectEnvVars: {
						Status: tasks.Info,
						Payload: map[string]string{
							"COMPlus_DbgEnableMiniDump": "1",
							"COMPlus_DbgMiniDumpType":   "2",
							"COMPlus_DbgMiniDumpName":   "/home/LogFiles/dumps/core.%p",
						},
					},
				}
			})
			It("should mark UsingLegacy=true and parse dump type", func() {
				cfg, ok := result.Payload.(*CrashDumpConfig)
				Expect(ok).To(BeTrue())
				Expect(cfg.Enabled).To(BeTrue())
				Expect(cfg.UsingLegacy).To(BeTrue())
				Expect(cfg.DumpType).To(Equal("Heap"))
			})
		})

		Context("when dump files are found on disk", func() {
			BeforeEach(func() {
				p.dumpDirScanner = mockDumpDirScanner([]DumpFile{
					{Name: testDumpFileName, Path: "/home/LogFiles/dumps/" + testDumpFileName, SizeMB: 256.5},
				})
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.Info},
					upstreamDetectRuntime:     {Status: tasks.Info, Payload: runtimeDotnetIsolated},
					upstreamCollectEnvVars: {
						Status: tasks.Info,
						Payload: map[string]string{
							"DOTNET_DbgEnableMiniDump": "1",
							"DOTNET_DbgMiniDumpType":   "4",
							"DOTNET_DbgMiniDumpName":   defaultDumpPath,
						},
					},
				}
			})
			It("should include the found files in the payload", func() {
				cfg, ok := result.Payload.(*CrashDumpConfig)
				Expect(ok).To(BeTrue())
				Expect(cfg.DumpFilesFound).To(HaveLen(1))
				Expect(cfg.DumpFilesFound[0].Name).To(Equal(testDumpFileName))
			})
			It("should mention the dump file in the summary", func() {
				Expect(result.Summary).To(ContainSubstring(testDumpFileName))
			})
		})

		Context("when runtime is node", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.Info},
					upstreamDetectRuntime:     {Status: tasks.Info, Payload: "node"},
					upstreamCollectEnvVars:    {Status: tasks.Info, Payload: map[string]string{}},
				}
			})
			It("should return None (no crash-time dump mechanism for Node.js)", func() {
				Expect(result.Status).To(Equal(tasks.None))
			})
		})

		Context("when runtime is python", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.Info},
					upstreamDetectRuntime:     {Status: tasks.Info, Payload: "python"},
					upstreamCollectEnvVars:    {Status: tasks.Info, Payload: map[string]string{}},
				}
			})
			It("should return None (no crash-time dump mechanism for Python)", func() {
				Expect(result.Status).To(Equal(tasks.None))
			})
		})

		Context("when runtime is java and JVM OOM dump is NOT configured", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.Info},
					upstreamDetectRuntime:     {Status: tasks.Info, Payload: "java"},
					upstreamCollectEnvVars:    {Status: tasks.Info, Payload: map[string]string{}},
				}
			})
			It("should return Warning", func() {
				Expect(result.Status).To(Equal(tasks.Warning))
			})
			It("should say JVM heap dump is not configured", func() {
				Expect(result.Summary).To(ContainSubstring("NOT configured"))
			})
			It("should have payload with Enabled=false and DumpType=JVM Heap", func() {
				cfg, ok := result.Payload.(*CrashDumpConfig)
				Expect(ok).To(BeTrue())
				Expect(cfg.Enabled).To(BeFalse())
				Expect(cfg.DumpType).To(Equal("JVM Heap"))
			})
		})

		Context("when runtime is java and JAVA_TOOL_OPTIONS contains OOM heap dump flags", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.Info},
					upstreamDetectRuntime:     {Status: tasks.Info, Payload: "java"},
					upstreamCollectEnvVars: {
						Status: tasks.Info,
						Payload: map[string]string{
							"JAVA_TOOL_OPTIONS": "-XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=" + jvmHeapDumpDir + " -XX:+ExitOnOutOfMemoryError",
						},
					},
				}
			})
			It(shouldReturnInfo, func() {
				Expect(result.Status).To(Equal(tasks.Info))
			})
			It("should report JVM heap dump as enabled with correct path", func() {
				cfg, ok := result.Payload.(*CrashDumpConfig)
				Expect(ok).To(BeTrue())
				Expect(cfg.Enabled).To(BeTrue())
				Expect(cfg.DumpPath).To(Equal(jvmHeapDumpDir))
				Expect(cfg.DumpType).To(Equal("JVM Heap"))
			})
		})

		Context("when runtime is java and JAVA_OPTS contains OOM heap dump flags", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.Info},
					upstreamDetectRuntime:     {Status: tasks.Info, Payload: "java"},
					upstreamCollectEnvVars: {
						Status: tasks.Info,
						Payload: map[string]string{
							"JAVA_OPTS": "-javaagent:/newrelic/newrelic.jar -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=" + jvmHeapDumpDir,
						},
					},
				}
			})
			It(shouldReturnInfo, func() {
				Expect(result.Status).To(Equal(tasks.Info))
			})
			It("should parse dump path from JAVA_OPTS", func() {
				cfg, ok := result.Payload.(*CrashDumpConfig)
				Expect(ok).To(BeTrue())
				Expect(cfg.Enabled).To(BeTrue())
				Expect(cfg.DumpPath).To(Equal(jvmHeapDumpDir))
			})
		})
	})

	Describe("parseCrashDumpConfig()", func() {
		It("returns disabled config when no dump vars are set", func() {
			cfg := parseCrashDumpConfig(map[string]string{})
			Expect(cfg.Enabled).To(BeFalse())
		})

		It("parses DOTNET_ vars correctly", func() {
			cfg := parseCrashDumpConfig(map[string]string{
				"DOTNET_DbgEnableMiniDump": "1",
				"DOTNET_DbgMiniDumpType":   "3",
				"DOTNET_DbgMiniDumpName":   "/tmp/dump",
			})
			Expect(cfg.Enabled).To(BeTrue())
			Expect(cfg.DumpType).To(Equal("Triage"))
			Expect(cfg.DumpPath).To(Equal("/tmp/dump"))
			Expect(cfg.UsingLegacy).To(BeFalse())
		})

		It("prefers DOTNET_ over COMPlus_ when both are set", func() {
			cfg := parseCrashDumpConfig(map[string]string{
				"DOTNET_DbgEnableMiniDump":  "1",
				"DOTNET_DbgMiniDumpType":    "4",
				"COMPlus_DbgEnableMiniDump": "1",
				"COMPlus_DbgMiniDumpType":   "1",
			})
			Expect(cfg.DumpType).To(Equal("Full"))
			Expect(cfg.UsingLegacy).To(BeFalse())
		})
	})
})
