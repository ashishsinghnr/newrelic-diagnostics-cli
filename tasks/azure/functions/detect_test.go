package functions

import (
	"github.com/newrelic/newrelic-diagnostics-cli/tasks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Azure/Functions/DetectFunctionApp", func() {
	var p AzureFunctionsDetectFunctionApp

	Describe("Identifier()", func() {
		It("should return correct identifier", func() {
			Expect(p.Identifier()).To(Equal(tasks.Identifier{
				Category:    "Azure",
				Subcategory: "Functions",
				Name:        "DetectFunctionApp",
			}))
		})
	})

	Describe("Explain()", func() {
		It("should return a non-empty explanation", func() {
			Expect(p.Explain()).NotTo(BeEmpty())
		})
	})

	Describe("Dependencies()", func() {
		It("should depend on Base/Env/CollectEnvVars and Base/Env/DetectAzure", func() {
			Expect(p.Dependencies()).To(ConsistOf(
				"Base/Env/CollectEnvVars",
				"Base/Env/DetectAzure",
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

		Context("when CollectEnvVars returns Warning", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					"Base/Env/CollectEnvVars": {Status: tasks.Warning},
					"Base/Env/DetectAzure":    {Status: tasks.Info},
				}
			})
			It("should return None", func() {
				Expect(result.Status).To(Equal(tasks.None))
			})
		})

		Context("when DetectAzure returns None (not Azure)", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					"Base/Env/CollectEnvVars": {Status: tasks.Info, Payload: map[string]string{}},
					"Base/Env/DetectAzure":    {Status: tasks.None},
				}
			})
			It("should return None", func() {
				Expect(result.Status).To(Equal(tasks.None))
			})
		})

		Context("when FUNCTIONS_WORKER_RUNTIME is absent (App Service, not Functions)", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					"Base/Env/CollectEnvVars": {
						Status:  tasks.Info,
						Payload: map[string]string{"WEBSITE_SITE_NAME": "my-app"},
					},
					"Base/Env/DetectAzure": {Status: tasks.Info},
				}
			})
			It("should return None", func() {
				Expect(result.Status).To(Equal(tasks.None))
			})
		})

		Context("when FUNCTIONS_WORKER_RUNTIME is present", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					"Base/Env/CollectEnvVars": {
						Status: tasks.Info,
						Payload: map[string]string{
							"WEBSITE_SITE_NAME":         "my-func-app",
							"FUNCTIONS_WORKER_RUNTIME":  "dotnet-isolated",
							"FUNCTIONS_EXTENSION_VERSION": "~4",
							"WEBSITE_RESOURCE_GROUP":    "my-rg",
						},
					},
					"Base/Env/DetectAzure": {Status: tasks.Info},
				}
			})
			It("should return Info", func() {
				Expect(result.Status).To(Equal(tasks.Info))
			})
			It("should include relevant env vars in the payload", func() {
				payload, ok := result.Payload.(map[string]string)
				Expect(ok).To(BeTrue())
				Expect(payload["FUNCTIONS_WORKER_RUNTIME"]).To(Equal("dotnet-isolated"))
				Expect(payload["WEBSITE_SITE_NAME"]).To(Equal("my-func-app"))
			})
		})
	})
})
