package functions

import (
	"os"
	"path/filepath"

	"github.com/newrelic/newrelic-diagnostics-cli/tasks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Azure/Functions/CollectReport", func() {
	var p AzureFunctionsCollectReport

	Describe("Identifier()", func() {
		It("should return correct identifier", func() {
			Expect(p.Identifier()).To(Equal(tasks.Identifier{
				Category:    "Azure",
				Subcategory: "Functions",
				Name:        "CollectReport",
			}))
		})
	})

	Describe("Dependencies()", func() {
		It("should include all diagnostic upstream tasks", func() {
			Expect(p.Dependencies()).To(ConsistOf(
				"Azure/Functions/DetectFunctionApp",
				"Azure/Functions/FetchAppSettings",
				"Azure/Functions/DetectRuntime",
				"Azure/Functions/ValidateAgentConfig",
				"Azure/Functions/CheckCrashDumpConfig",
				"Azure/Functions/AgentInfo",
				"Azure/Functions/AnalyzeLogs",
			))
		})
	})

	Describe("Execute()", func() {
		var (
			result   tasks.Result
			options  tasks.Options
			upstream map[string]tasks.Result
			outputDir string
		)

		JustBeforeEach(func() {
			result = p.Execute(options, upstream)
		})

		Context("when not in an Azure Function App", func() {
			BeforeEach(func() {
				options = tasks.Options{Options: map[string]string{}}
				upstream = map[string]tasks.Result{
					"Azure/Functions/DetectFunctionApp": {Status: tasks.None},
				}
			})
			It("should return None", func() {
				Expect(result.Status).To(Equal(tasks.None))
			})
		})

		Context("when in an Azure Function App", func() {
			BeforeEach(func() {
				var err error
				outputDir, err = os.MkdirTemp("", "nrdiag-report-test-*")
				Expect(err).NotTo(HaveOccurred())

				options = tasks.Options{Options: map[string]string{
					"outputPath": outputDir,
				}}
				upstream = map[string]tasks.Result{
					"Azure/Functions/DetectFunctionApp":   {Status: tasks.Info, Summary: "Detected Azure Function App"},
					"Azure/Functions/DetectRuntime":       {Status: tasks.Info, Summary: "Runtime: dotnet-isolated"},
					"Azure/Functions/ValidateAgentConfig": {Status: tasks.Success, Summary: "All required config vars present"},
					"Azure/Functions/CheckCrashDumpConfig": {Status: tasks.Info, Summary: "Crash dump NOT configured"},
					"Azure/Functions/AgentInfo":           {Status: tasks.Info, Summary: "Found 3 New Relic env vars"},
					"Azure/Functions/AnalyzeLogs":         {Status: tasks.Success, Summary: "No errors found"},
				}
			})

			AfterEach(func() {
				os.RemoveAll(outputDir)
			})

			It("should return Info", func() {
				Expect(result.Status).To(Equal(tasks.Info))
			})
			It("should mention the report file path in summary", func() {
				Expect(result.Summary).To(ContainSubstring(reportFilename))
			})
			It("should create the report file on disk", func() {
				reportPath := filepath.Join(outputDir, reportFilename)
				_, err := os.Stat(reportPath)
				Expect(err).NotTo(HaveOccurred())
			})
			It("should include task statuses in the report content", func() {
				reportPath := filepath.Join(outputDir, reportFilename)
				content, err := os.ReadFile(reportPath)
				Expect(err).NotTo(HaveOccurred())
				Expect(string(content)).To(ContainSubstring("ValidateAgentConfig"))
				Expect(string(content)).To(ContainSubstring("Success"))
				Expect(string(content)).To(ContainSubstring("CheckCrashDumpConfig"))
			})
			It("should include the report in FilesToCopy", func() {
				Expect(result.FilesToCopy).To(HaveLen(1))
				Expect(result.FilesToCopy[0].Identifier).To(Equal("Azure/Functions/CollectReport"))
			})
		})
	})

	Describe("buildReport()", func() {
		It("includes all task names and statuses", func() {
			upstream := map[string]tasks.Result{
				"Azure/Functions/DetectFunctionApp":   {Status: tasks.Info, Summary: "detected"},
				"Azure/Functions/DetectRuntime":       {Status: tasks.Info, Summary: "dotnet-isolated"},
				"Azure/Functions/ValidateAgentConfig": {Status: tasks.Success, Summary: "ok"},
				"Azure/Functions/CheckCrashDumpConfig": {Status: tasks.Info, Summary: "not configured"},
				"Azure/Functions/AgentInfo":           {Status: tasks.Warning, Summary: "no vars found"},
				"Azure/Functions/AnalyzeLogs":         {Status: tasks.Failure, Summary: "errors found"},
			}
			report := buildReport(upstream)
			Expect(report).To(ContainSubstring("DetectFunctionApp"))
			Expect(report).To(ContainSubstring("Warning"))
			Expect(report).To(ContainSubstring("Failure"))
			Expect(report).To(ContainSubstring("Azure Functions New Relic Diagnostic Report"))
		})
	})
})
