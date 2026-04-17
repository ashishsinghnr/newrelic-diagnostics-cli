package functions

import (
	"encoding/json"
	"fmt"

	"github.com/newrelic/newrelic-diagnostics-cli/tasks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	shouldReturnNone   = "should return None"
	configureFuncName  = "my-func"
	configureRGName    = "my-rg"
)

var _ = Describe("Azure/Functions/ConfigureCrashDump", func() {
	var p AzureFunctionsConfigureCrashDump

	BeforeEach(func() {
		p = AzureFunctionsConfigureCrashDump{}
	})

	Describe("Identifier()", func() {
		It("should return correct identifier", func() {
			Expect(p.Identifier()).To(Equal(tasks.Identifier{
				Category:    "Azure",
				Subcategory: "Functions",
				Name:        "ConfigureCrashDump",
			}))
		})
	})

	Describe("Dependencies()", func() {
		It("should depend on CollectLiveMemoryDump and DetectRuntime", func() {
			Expect(p.Dependencies()).To(ConsistOf(
				"Azure/Functions/CollectLiveMemoryDump",
				"Azure/Functions/DownloadSiteDump",
				"Azure/Functions/DetectRuntime",
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

		Context("when functionName or resourceGroup are not provided", func() {
			BeforeEach(func() {
				options = tasks.Options{Options: map[string]string{}}
				upstream = map[string]tasks.Result{}
			})
			It(shouldReturnNone, func() {
				Expect(result.Status).To(Equal(tasks.None))
			})
		})

		Context("when YesToAll and az CLI succeeds (dotnet-isolated runtime)", func() {
			BeforeEach(func() {
				// az returns a JSON array of the updated settings on success.
				fakeOutput, _ := json.Marshal([]map[string]string{
					{"name": "DOTNET_DbgEnableMiniDump", "value": "1"},
					{"name": "DOTNET_DbgMiniDumpType", "value": "4"},
					{"name": "DOTNET_DbgMiniDumpName", "value": "/home/LogFiles/dumps/coredump.%p.%t"},
				})
				p.cmdRunner = mockCmdRunner(
					map[string][]byte{"functionapp": fakeOutput},
					nil,
				)
				options = tasks.Options{Options: map[string]string{
					"functionName":  configureFuncName,
					"resourceGroup": configureRGName,
					"YesToAll":      "true",
				}}
				upstream = map[string]tasks.Result{
					upstreamDetectRuntime: {Status: tasks.Info, Payload: runtimeDotnetIsolated},
				}
			})

			It("should return Success", func() {
				Expect(result.Status).To(Equal(tasks.Success))
			})
			It("should mention the function app name in the summary", func() {
				Expect(result.Summary).To(ContainSubstring(configureFuncName))
			})
			It("should list the applied settings in the summary", func() {
				Expect(result.Summary).To(ContainSubstring("DOTNET_DbgEnableMiniDump"))
				Expect(result.Summary).To(ContainSubstring("DOTNET_DbgMiniDumpType"))
				Expect(result.Summary).To(ContainSubstring("DOTNET_DbgMiniDumpName"))
			})
			It("should mention restart in the summary", func() {
				Expect(result.Summary).To(ContainSubstring("restart"))
			})
			It("should populate the payload with ConfigureCrashDumpResult", func() {
				cfg, ok := result.Payload.(*ConfigureCrashDumpResult)
				Expect(ok).To(BeTrue())
				Expect(cfg.FunctionAppName).To(Equal(configureFuncName))
				Expect(cfg.ResourceGroup).To(Equal(configureRGName))
				Expect(cfg.AppliedSettings).To(HaveKey("DOTNET_DbgEnableMiniDump"))
			})
		})

		Context("when az CLI fails (dotnet runtime)", func() {
			BeforeEach(func() {
				p.cmdRunner = mockCmdRunner(
					nil,
					map[string]error{"functionapp": fmt.Errorf("resource group not found")},
				)
				options = tasks.Options{Options: map[string]string{
					"functionName":  configureFuncName,
					"resourceGroup": "bad-rg",
					"YesToAll":      "true",
				}}
				upstream = map[string]tasks.Result{
					upstreamDetectRuntime: {Status: tasks.Info, Payload: runtimeDotnetIsolated},
				}
			})

			It("should return Error", func() {
				Expect(result.Status).To(Equal(tasks.Error))
			})
			It("should mention the failure in the summary", func() {
				Expect(result.Summary).To(ContainSubstring("Failed to apply"))
			})
		})

		Context("when runtime is java and az CLI succeeds", func() {
			BeforeEach(func() {
				fakeOutput, _ := json.Marshal([]map[string]string{
					{"name": "JAVA_TOOL_OPTIONS", "value": "-XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/home/LogFiles/dumps/ -XX:+ExitOnOutOfMemoryError"},
				})
				p.cmdRunner = mockCmdRunner(
					map[string][]byte{"functionapp": fakeOutput},
					nil,
				)
				options = tasks.Options{Options: map[string]string{
					"functionName":  configureFuncName,
					"resourceGroup": configureRGName,
					"YesToAll":      "true",
				}}
				upstream = map[string]tasks.Result{
					upstreamDetectRuntime: {Status: tasks.Info, Payload: "java"},
				}
			})

			It("should return Success", func() {
				Expect(result.Status).To(Equal(tasks.Success))
			})
			It("should list JAVA_TOOL_OPTIONS in the summary", func() {
				Expect(result.Summary).To(ContainSubstring("JAVA_TOOL_OPTIONS"))
			})
			It("should populate AppliedSettings with JAVA_TOOL_OPTIONS", func() {
				cfg, ok := result.Payload.(*ConfigureCrashDumpResult)
				Expect(ok).To(BeTrue())
				Expect(cfg.AppliedSettings).To(HaveKey("JAVA_TOOL_OPTIONS"))
			})
		})

		Context("when runtime is node (no dump config available)", func() {
			BeforeEach(func() {
				options = tasks.Options{Options: map[string]string{
					"functionName":  configureFuncName,
					"resourceGroup": configureRGName,
					"YesToAll":      "true",
				}}
				upstream = map[string]tasks.Result{
					upstreamDetectRuntime: {Status: tasks.Info, Payload: "node"},
				}
			})

			It(shouldReturnNone, func() {
				Expect(result.Status).To(Equal(tasks.None))
			})
			It("should mention the runtime in the summary", func() {
				Expect(result.Summary).To(ContainSubstring("node"))
			})
		})

		Context("when runtime is python (no dump config available)", func() {
			BeforeEach(func() {
				options = tasks.Options{Options: map[string]string{
					"functionName":  configureFuncName,
					"resourceGroup": configureRGName,
					"YesToAll":      "true",
				}}
				upstream = map[string]tasks.Result{
					upstreamDetectRuntime: {Status: tasks.Info, Payload: "python"},
				}
			})

			It(shouldReturnNone, func() {
				Expect(result.Status).To(Equal(tasks.None))
			})
		})
	})

	Describe("applyAppSettings()", func() {
		It("succeeds when az CLI returns valid JSON", func() {
			out, _ := json.Marshal([]interface{}{})
			runner := mockCmdRunner(map[string][]byte{"functionapp": out}, nil)
			err := applyAppSettings(runner, "func", "rg", map[string]string{"KEY": "val"})
			Expect(err).NotTo(HaveOccurred())
		})

		It("returns error when az CLI fails", func() {
			runner := mockCmdRunner(nil, map[string]error{"functionapp": fmt.Errorf("not found")})
			err := applyAppSettings(runner, "func", "rg", map[string]string{"KEY": "val"})
			Expect(err).To(HaveOccurred())
		})
	})
})
