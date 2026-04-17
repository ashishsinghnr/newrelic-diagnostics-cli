package functions

import (
	"github.com/newrelic/newrelic-diagnostics-cli/tasks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Azure/Functions/DetectRuntime", func() {
	var p AzureFunctionsDetectRuntime

	Describe("Identifier()", func() {
		It("should return correct identifier", func() {
			Expect(p.Identifier()).To(Equal(tasks.Identifier{
				Category:    "Azure",
				Subcategory: "Functions",
				Name:        "DetectRuntime",
			}))
		})
	})

	Describe("Dependencies()", func() {
		It("should depend on Azure/Functions/DetectFunctionApp", func() {
			Expect(p.Dependencies()).To(ConsistOf(
				"Azure/Functions/DetectFunctionApp",
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

		Context("when DetectFunctionApp returns None", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					"Azure/Functions/DetectFunctionApp": {Status: tasks.None},
				}
			})
			It("should return None", func() {
				Expect(result.Status).To(Equal(tasks.None))
			})
		})

		Context("when runtime is dotnet-isolated", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					"Azure/Functions/DetectFunctionApp": {
						Status: tasks.Info,
						Payload: map[string]string{
							"FUNCTIONS_WORKER_RUNTIME": "dotnet-isolated",
						},
					},
				}
			})
			It("should return Info", func() {
				Expect(result.Status).To(Equal(tasks.Info))
			})
			It("should set payload to the runtime string", func() {
				Expect(result.Payload).To(Equal("dotnet-isolated"))
			})
		})

		Context("when runtime is python (case-insensitive)", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					"Azure/Functions/DetectFunctionApp": {
						Status:  tasks.Info,
						Payload: map[string]string{"FUNCTIONS_WORKER_RUNTIME": "Python"},
					},
				}
			})
			It("should return Info", func() {
				Expect(result.Status).To(Equal(tasks.Info))
			})
			It("should normalise runtime to lower case", func() {
				Expect(result.Payload).To(Equal("python"))
			})
		})

		Context("when runtime is an unknown value", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					"Azure/Functions/DetectFunctionApp": {
						Status:  tasks.Info,
						Payload: map[string]string{"FUNCTIONS_WORKER_RUNTIME": "cobol"},
					},
				}
			})
			It("should return Warning", func() {
				Expect(result.Status).To(Equal(tasks.Warning))
			})
		})

		Context("when FUNCTIONS_WORKER_RUNTIME is empty", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					"Azure/Functions/DetectFunctionApp": {
						Status:  tasks.Info,
						Payload: map[string]string{"FUNCTIONS_WORKER_RUNTIME": ""},
					},
				}
			})
			It("should return Warning", func() {
				Expect(result.Status).To(Equal(tasks.Warning))
			})
		})
	})

	Describe("IsDotnetRuntime()", func() {
		DescribeTable("returns true only for dotnet runtimes",
			func(runtime string, expected bool) {
				Expect(IsDotnetRuntime(runtime)).To(Equal(expected))
			},
			Entry("dotnet", "dotnet", true),
			Entry("dotnet-isolated", "dotnet-isolated", true),
			Entry("DOTNET", "DOTNET", true),
			Entry("node", "node", false),
			Entry("python", "python", false),
			Entry("java", "java", false),
			Entry("powershell", "powershell", false),
			Entry("empty", "", false),
		)
	})

	Describe("IsJavaRuntime()", func() {
		DescribeTable("returns true only for java runtime",
			func(runtime string, expected bool) {
				Expect(IsJavaRuntime(runtime)).To(Equal(expected))
			},
			Entry("java", "java", true),
			Entry("JAVA", "JAVA", true),
			Entry("dotnet", "dotnet", false),
			Entry("node", "node", false),
			Entry("python", "python", false),
			Entry("empty", "", false),
		)
	})

	Describe("IsNodeRuntime()", func() {
		DescribeTable("returns true only for node runtime",
			func(runtime string, expected bool) {
				Expect(IsNodeRuntime(runtime)).To(Equal(expected))
			},
			Entry("node", "node", true),
			Entry("NODE", "NODE", true),
			Entry("dotnet", "dotnet", false),
			Entry("java", "java", false),
			Entry("python", "python", false),
			Entry("empty", "", false),
		)
	})

	Describe("IsPythonRuntime()", func() {
		DescribeTable("returns true only for python runtime",
			func(runtime string, expected bool) {
				Expect(IsPythonRuntime(runtime)).To(Equal(expected))
			},
			Entry("python", "python", true),
			Entry("PYTHON", "PYTHON", true),
			Entry("dotnet", "dotnet", false),
			Entry("java", "java", false),
			Entry("node", "node", false),
			Entry("empty", "", false),
		)
	})
})
