package functions

import (
	"github.com/newrelic/newrelic-diagnostics-cli/tasks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Azure/Functions/AgentInfo", func() {
	var p AzureFunctionsAgentInfo

	Describe("Identifier()", func() {
		It("should return correct identifier", func() {
			Expect(p.Identifier()).To(Equal(tasks.Identifier{
				Category:    "Azure",
				Subcategory: "Functions",
				Name:        "AgentInfo",
			}))
		})
	})

	Describe("Dependencies()", func() {
		It("should include all required upstream tasks", func() {
			Expect(p.Dependencies()).To(ConsistOf(
				"Azure/Functions/DetectFunctionApp",
				"Azure/Functions/DetectRuntime",
				"Base/Env/CollectEnvVars",
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

		Context("when not in a Function App", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					"Azure/Functions/DetectFunctionApp": {Status: tasks.None},
					"Azure/Functions/DetectRuntime":     {Status: tasks.None},
					"Base/Env/CollectEnvVars":           {Status: tasks.Info, Payload: map[string]string{}},
				}
			})
			It("should return None", func() {
				Expect(result.Status).To(Equal(tasks.None))
			})
		})

		Context("when no NR env vars are present", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					"Azure/Functions/DetectFunctionApp": {Status: tasks.Info},
					"Azure/Functions/DetectRuntime":     {Status: tasks.Info, Payload: "node"},
					"Base/Env/CollectEnvVars": {
						Status:  tasks.Info,
						Payload: map[string]string{"WEBSITE_SITE_NAME": "my-app"},
					},
				}
			})
			It("should return Warning", func() {
				Expect(result.Status).To(Equal(tasks.Warning))
			})
		})

		Context("when NR env vars are present for a Node runtime", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					"Azure/Functions/DetectFunctionApp": {Status: tasks.Info},
					"Azure/Functions/DetectRuntime":     {Status: tasks.Info, Payload: "node"},
					"Base/Env/CollectEnvVars": {
						Status: tasks.Info,
						Payload: map[string]string{
							"NEW_RELIC_LICENSE_KEY": "1234567890abcdef1234567890abcdef12345678",
							"NEW_RELIC_APP_NAME":    "my-node-app",
						},
					},
				}
			})
			It("should return Info", func() {
				Expect(result.Status).To(Equal(tasks.Info))
			})
			It("should mask the license key in the payload", func() {
				payload, ok := result.Payload.(map[string]string)
				Expect(ok).To(BeTrue())
				Expect(payload["NEW_RELIC_LICENSE_KEY"]).To(HaveSuffix("****"))
				Expect(payload["NEW_RELIC_LICENSE_KEY"]).To(HaveLen(8)) // 4 chars + "****"
			})
			It("should not mask the app name", func() {
				payload, ok := result.Payload.(map[string]string)
				Expect(ok).To(BeTrue())
				Expect(payload["NEW_RELIC_APP_NAME"]).To(Equal("my-node-app"))
			})
			It("should not include CORECLR vars for non-.NET runtime", func() {
				payload, ok := result.Payload.(map[string]string)
				Expect(ok).To(BeTrue())
				Expect(payload).NotTo(HaveKey("CORECLR_ENABLE_PROFILING"))
			})
		})

		Context("when runtime is dotnet-isolated with CORECLR vars", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					"Azure/Functions/DetectFunctionApp": {Status: tasks.Info},
					"Azure/Functions/DetectRuntime":     {Status: tasks.Info, Payload: "dotnet-isolated"},
					"Base/Env/CollectEnvVars": {
						Status: tasks.Info,
						Payload: map[string]string{
							"NEW_RELIC_LICENSE_KEY":    "1234567890abcdef1234567890abcdef12345678",
							"NEW_RELIC_APP_NAME":       "my-dotnet-app",
							"CORECLR_ENABLE_PROFILING": "1",
							"CORECLR_PROFILER":         "{36032161-FFC0-4B61-B559-F6C5D41BAE5A}",
						},
					},
				}
			})
			It("should include CORECLR_ENABLE_PROFILING in the payload", func() {
				payload, ok := result.Payload.(map[string]string)
				Expect(ok).To(BeTrue())
				Expect(payload).To(HaveKey("CORECLR_ENABLE_PROFILING"))
				Expect(payload).To(HaveKey("CORECLR_PROFILER"))
			})
		})
	})

	Describe("maskIfSensitive()", func() {
		DescribeTable("masks sensitive values and leaves others unchanged",
			func(key, val, expected string) {
				Expect(maskIfSensitive(key, val)).To(Equal(expected))
			},
			Entry("license key (long)", "NEW_RELIC_LICENSE_KEY", "1234567890abcdef1234", "1234****"),
			Entry("license key (short)", "NEW_RELIC_LICENSE_KEY", "shor", "****"),
			Entry("token", "MY_TOKEN", "abcdefghijklmnop", "abcd****"),
			Entry("obfuscated proxy pass", "NEW_RELIC_PROXY_PASS_OBFUSCATED", "obfuscatedvalue123", "obfu****"),
			Entry("app name (not sensitive)", "NEW_RELIC_APP_NAME", "my-app", "my-app"),
			Entry("non-sensitive var", "NEW_RELIC_LOG_LEVEL", "debug", "debug"),
		)
	})
})
