package functions

import (
	"github.com/newrelic/newrelic-diagnostics-cli/tasks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	validLicenseKey   = "1234567890abcdef1234567890abcdef12345678"
	shouldReturnSuccess = "should return Success"
	shouldReturnWarning = "should return Warning"
	appNameNode       = "my-node-app"
	appNameJava       = "my-java-app"
	appNamePython     = "my-python-app"
)

var _ = Describe("Azure/Functions/ValidateAgentConfig", func() {
	var p AzureFunctionsValidateAgentConfig

	Describe("Identifier()", func() {
		It("should return correct identifier", func() {
			Expect(p.Identifier()).To(Equal(tasks.Identifier{
				Category:    "Azure",
				Subcategory: "Functions",
				Name:        "ValidateAgentConfig",
			}))
		})
	})

	Describe("Dependencies()", func() {
		It("should include all three upstream tasks", func() {
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
					upstreamCollectEnvVars:    {Status: tasks.Info, Payload: map[string]string{}},
				}
			})
			It("should return None", func() {
				Expect(result.Status).To(Equal(tasks.None))
			})
		})

		Context("when license key and app name are missing", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.Info},
					upstreamDetectRuntime:     {Status: tasks.Info, Payload: "node"},
					upstreamCollectEnvVars:    {Status: tasks.Info, Payload: map[string]string{}},
				}
			})
			It(shouldReturnFailure, func() {
				Expect(result.Status).To(Equal(tasks.Failure))
			})
			It("should mention the missing license key", func() {
				Expect(result.Summary).To(ContainSubstring("NEW_RELIC_LICENSE_KEY"))
			})
		})

		Context("when license key is valid but app name is missing", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.Info},
					upstreamDetectRuntime:     {Status: tasks.Info, Payload: "node"},
					upstreamCollectEnvVars: {
						Status:  tasks.Info,
						Payload: map[string]string{"NEW_RELIC_LICENSE_KEY": validLicenseKey},
					},
				}
			})
			It("should return Warning (not Failure) since the agent still functions", func() {
				Expect(result.Status).To(Equal(tasks.Warning))
			})
			It("should mention NEW_RELIC_APP_NAME", func() {
				Expect(result.Summary).To(ContainSubstring("NEW_RELIC_APP_NAME"))
			})
		})

		Context("when license key has wrong format", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.Info},
					upstreamDetectRuntime:     {Status: tasks.Info, Payload: "node"},
					upstreamCollectEnvVars: {
						Status: tasks.Info,
						Payload: map[string]string{
							"NEW_RELIC_LICENSE_KEY": "tooshort",
							"NEW_RELIC_APP_NAME":    "my-app",
						},
					},
				}
			})
			It(shouldReturnFailure, func() {
				Expect(result.Status).To(Equal(tasks.Failure))
			})
		})

		Context("when config is complete for a Node.js runtime", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.Info},
					upstreamDetectRuntime:     {Status: tasks.Info, Payload: "node"},
					upstreamCollectEnvVars: {
						Status: tasks.Info,
						Payload: map[string]string{
							"NEW_RELIC_LICENSE_KEY":    validLicenseKey,
							"NEW_RELIC_APP_NAME":       appNameNode,
							"NEW_RELIC_NO_CONFIG_FILE": "true",
							"NODE_OPTIONS":             "--require newrelic",
						},
					},
				}
			})
			It(shouldReturnSuccess, func() {
				Expect(result.Status).To(Equal(tasks.Success))
			})
		})

		Context("when config is complete for a dotnet-isolated runtime", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.Info},
					upstreamDetectRuntime:     {Status: tasks.Info, Payload: runtimeDotnetIsolated},
					upstreamCollectEnvVars: {
						Status: tasks.Info,
						Payload: map[string]string{
							"NEW_RELIC_LICENSE_KEY":    validLicenseKey,
							"NEW_RELIC_APP_NAME":       "my-dotnet-app",
							"CORECLR_ENABLE_PROFILING": "1",
							"CORECLR_NEWRELIC_HOME":   "/home/site/wwwroot/newrelic",
							"CORECLR_PROFILER":         "{36032161-FFC0-4B61-B559-F6C5D41BAE5A}",
							"CORECLR_PROFILER_PATH":    "/home/site/wwwroot/newrelic/libNewRelicProfiler.so",
						},
					},
				}
			})
			It(shouldReturnSuccess, func() {
				Expect(result.Status).To(Equal(tasks.Success))
			})
		})

		Context("when dotnet runtime has a malformed or wrong CORECLR_PROFILER GUID", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.Info},
					upstreamDetectRuntime:     {Status: tasks.Info, Payload: runtimeDotnetIsolated},
					upstreamCollectEnvVars: {
						Status: tasks.Info,
						Payload: map[string]string{
							"NEW_RELIC_LICENSE_KEY":    validLicenseKey,
							"NEW_RELIC_APP_NAME":       "my-dotnet-app",
							"CORECLR_ENABLE_PROFILING": "1",
							"CORECLR_NEWRELIC_HOME":    "/home/site/wwwroot/newrelic",
							"CORECLR_PROFILER":         "{36032161-FFC0-4B61-B559-F6C5D41BAE5A", // missing closing brace
							"CORECLR_PROFILER_PATH":    "/home/site/wwwroot/newrelic/libNewRelicProfiler.so",
						},
					},
				}
			})
			It(shouldReturnFailure, func() {
				Expect(result.Status).To(Equal(tasks.Failure))
			})
			It("should mention CORECLR_PROFILER in the failure message", func() {
				Expect(result.Summary).To(ContainSubstring("CORECLR_PROFILER"))
			})
		})

		Context("when dotnet runtime is missing CORECLR_ENABLE_PROFILING", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.Info},
					upstreamDetectRuntime:     {Status: tasks.Info, Payload: "dotnet"},
					upstreamCollectEnvVars: {
						Status: tasks.Info,
						Payload: map[string]string{
							"NEW_RELIC_LICENSE_KEY": validLicenseKey,
							"NEW_RELIC_APP_NAME":    "my-dotnet-app",
						},
					},
				}
			})
			It(shouldReturnFailure, func() {
				Expect(result.Status).To(Equal(tasks.Failure))
			})
			It("should mention CORECLR_ENABLE_PROFILING", func() {
				Expect(result.Summary).To(ContainSubstring("CORECLR_ENABLE_PROFILING"))
			})
		})

		Context("when runtime is java and JAVA_OPTS is missing", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.Info},
					upstreamDetectRuntime:     {Status: tasks.Info, Payload: "java"},
					upstreamCollectEnvVars: {
						Status: tasks.Info,
						Payload: map[string]string{
							"NEW_RELIC_LICENSE_KEY": validLicenseKey,
							"NEW_RELIC_APP_NAME":    appNameJava,
						},
					},
				}
			})
			It(shouldReturnWarning, func() {
				Expect(result.Status).To(Equal(tasks.Warning))
			})
			It("should mention JAVA_OPTS", func() {
				Expect(result.Summary).To(ContainSubstring("JAVA_OPTS"))
			})
		})

		Context("when runtime is java and JAVA_OPTS contains -javaagent:newrelic.jar", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.Info},
					upstreamDetectRuntime:     {Status: tasks.Info, Payload: "java"},
					upstreamCollectEnvVars: {
						Status: tasks.Info,
						Payload: map[string]string{
							"NEW_RELIC_LICENSE_KEY": validLicenseKey,
							"NEW_RELIC_APP_NAME":    appNameJava,
							"JAVA_OPTS":             "-javaagent:/home/site/wwwroot/newrelic/newrelic.jar",
						},
					},
				}
			})
			It(shouldReturnSuccess, func() {
				Expect(result.Status).To(Equal(tasks.Success))
			})
			It("should mention JAVA_OPTS in summary", func() {
				Expect(result.Summary).To(ContainSubstring("JAVA_OPTS"))
			})
		})

		Context("when runtime is java and JAVA_OPTS has -javaagent but no newrelic reference", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.Info},
					upstreamDetectRuntime:     {Status: tasks.Info, Payload: "java"},
					upstreamCollectEnvVars: {
						Status: tasks.Info,
						Payload: map[string]string{
							"NEW_RELIC_LICENSE_KEY": validLicenseKey,
							"NEW_RELIC_APP_NAME":    appNameJava,
							"JAVA_OPTS":             "-javaagent:/other/agent.jar",
						},
					},
				}
			})
			It(shouldReturnWarning, func() {
				Expect(result.Status).To(Equal(tasks.Warning))
			})
		})

		Context("when runtime is node and NEW_RELIC_NO_CONFIG_FILE is missing", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.Info},
					upstreamDetectRuntime:     {Status: tasks.Info, Payload: "node"},
					upstreamCollectEnvVars: {
						Status: tasks.Info,
						Payload: map[string]string{
							"NEW_RELIC_LICENSE_KEY": validLicenseKey,
							"NEW_RELIC_APP_NAME":    appNameNode,
						},
					},
				}
			})
			It(shouldReturnWarning, func() {
				Expect(result.Status).To(Equal(tasks.Warning))
			})
			It("should mention NEW_RELIC_NO_CONFIG_FILE", func() {
				Expect(result.Summary).To(ContainSubstring("NEW_RELIC_NO_CONFIG_FILE"))
			})
		})

		Context("when runtime is node and NEW_RELIC_NO_CONFIG_FILE=true with NODE_OPTIONS set", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.Info},
					upstreamDetectRuntime:     {Status: tasks.Info, Payload: "node"},
					upstreamCollectEnvVars: {
						Status: tasks.Info,
						Payload: map[string]string{
							"NEW_RELIC_LICENSE_KEY":    validLicenseKey,
							"NEW_RELIC_APP_NAME":       appNameNode,
							"NEW_RELIC_NO_CONFIG_FILE": "true",
							"NODE_OPTIONS":             "--require newrelic",
						},
					},
				}
			})
			It(shouldReturnSuccess, func() {
				Expect(result.Status).To(Equal(tasks.Success))
			})
		})

		Context("when runtime is node and NODE_OPTIONS is missing --require newrelic", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.Info},
					upstreamDetectRuntime:     {Status: tasks.Info, Payload: "node"},
					upstreamCollectEnvVars: {
						Status: tasks.Info,
						Payload: map[string]string{
							"NEW_RELIC_LICENSE_KEY":    validLicenseKey,
							"NEW_RELIC_APP_NAME":       appNameNode,
							"NEW_RELIC_NO_CONFIG_FILE": "true",
						},
					},
				}
			})
			It(shouldReturnWarning, func() {
				Expect(result.Status).To(Equal(tasks.Warning))
			})
			It("should mention NODE_OPTIONS", func() {
				Expect(result.Summary).To(ContainSubstring("NODE_OPTIONS"))
			})
		})

		Context("when runtime is node and NODE_OPTIONS uses the -r shorthand", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.Info},
					upstreamDetectRuntime:     {Status: tasks.Info, Payload: "node"},
					upstreamCollectEnvVars: {
						Status: tasks.Info,
						Payload: map[string]string{
							"NEW_RELIC_LICENSE_KEY":    validLicenseKey,
							"NEW_RELIC_APP_NAME":       appNameNode,
							"NEW_RELIC_NO_CONFIG_FILE": "true",
							"NODE_OPTIONS":             "-r newrelic",
						},
					},
				}
			})
			It(shouldReturnSuccess, func() {
				Expect(result.Status).To(Equal(tasks.Success))
			})
		})

		Context("when runtime is python and PYTHONFAULTHANDLER is missing", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.Info},
					upstreamDetectRuntime:     {Status: tasks.Info, Payload: "python"},
					upstreamCollectEnvVars: {
						Status: tasks.Info,
						Payload: map[string]string{
							"NEW_RELIC_LICENSE_KEY": validLicenseKey,
							"NEW_RELIC_APP_NAME":    appNamePython,
						},
					},
				}
			})
			It(shouldReturnWarning, func() {
				Expect(result.Status).To(Equal(tasks.Warning))
			})
			It("should mention PYTHONFAULTHANDLER", func() {
				Expect(result.Summary).To(ContainSubstring("PYTHONFAULTHANDLER"))
			})
		})

		Context("when runtime is python and PYTHONFAULTHANDLER=1", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.Info},
					upstreamDetectRuntime:     {Status: tasks.Info, Payload: "python"},
					upstreamCollectEnvVars: {
						Status: tasks.Info,
						Payload: map[string]string{
							"NEW_RELIC_LICENSE_KEY": validLicenseKey,
							"NEW_RELIC_APP_NAME":    appNamePython,
							"PYTHONFAULTHANDLER":    "1",
						},
					},
				}
			})
			It(shouldReturnSuccess, func() {
				Expect(result.Status).To(Equal(tasks.Success))
			})
			It("should note env-only config mode when NEW_RELIC_CONFIG_FILE is not set", func() {
				Expect(result.Summary).To(ContainSubstring("NEW_RELIC_CONFIG_FILE"))
				Expect(result.Summary).To(ContainSubstring("environment-variable-only"))
			})
		})

		Context("when runtime is python and NEW_RELIC_CONFIG_FILE is set", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.Info},
					upstreamDetectRuntime:     {Status: tasks.Info, Payload: "python"},
					upstreamCollectEnvVars: {
						Status: tasks.Info,
						Payload: map[string]string{
							"NEW_RELIC_LICENSE_KEY":  validLicenseKey,
							"NEW_RELIC_APP_NAME":     appNamePython,
							"PYTHONFAULTHANDLER":     "1",
							"NEW_RELIC_CONFIG_FILE":  "/home/site/wwwroot/newrelic.ini",
						},
					},
				}
			})
			It(shouldReturnSuccess, func() {
				Expect(result.Status).To(Equal(tasks.Success))
			})
			It("should include the config file path in the summary", func() {
				Expect(result.Summary).To(ContainSubstring("newrelic.ini"))
			})
		})

		Context("when runtime is powershell (unsupported)", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.Info},
					upstreamDetectRuntime:     {Status: tasks.Info, Payload: "powershell"},
					upstreamCollectEnvVars: {
						Status: tasks.Info,
						Payload: map[string]string{
							"NEW_RELIC_LICENSE_KEY": validLicenseKey,
							"NEW_RELIC_APP_NAME":    "my-ps-app",
						},
					},
				}
			})
			It(shouldReturnSuccess, func() {
				Expect(result.Status).To(Equal(tasks.Success))
			})
			It("should note that the runtime has no first-party NR agent", func() {
				Expect(result.Summary).To(ContainSubstring("no first-party New Relic agent"))
			})
		})

		Context("when runtime is custom (unsupported handler)", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.Info},
					upstreamDetectRuntime:     {Status: tasks.Info, Payload: "custom"},
					upstreamCollectEnvVars: {
						Status: tasks.Info,
						Payload: map[string]string{
							"NEW_RELIC_LICENSE_KEY": validLicenseKey,
							"NEW_RELIC_APP_NAME":    "my-custom-app",
						},
					},
				}
			})
			It(shouldReturnSuccess, func() {
				Expect(result.Status).To(Equal(tasks.Success))
			})
		})

		Context("when Application Insights is also configured", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					upstreamDetectFunctionApp: {Status: tasks.Info},
					upstreamDetectRuntime:     {Status: tasks.Info, Payload: "node"},
					upstreamCollectEnvVars: {
						Status: tasks.Info,
						Payload: map[string]string{
							"NEW_RELIC_LICENSE_KEY":                 validLicenseKey,
							"NEW_RELIC_APP_NAME":                    "my-app",
							"APPLICATIONINSIGHTS_CONNECTION_STRING": "InstrumentationKey=abc123",
						},
					},
				}
			})
			It(shouldReturnWarning, func() {
				Expect(result.Status).To(Equal(tasks.Warning))
			})
			It("should mention Application Insights", func() {
				Expect(result.Summary).To(ContainSubstring("APPLICATIONINSIGHTS_CONNECTION_STRING"))
			})
		})
	})
})
