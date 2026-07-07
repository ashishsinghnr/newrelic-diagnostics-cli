package functions

import (
	"errors"

	"github.com/newrelic/newrelic-diagnostics-cli/tasks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// --- test helpers ---

func mockGlober(files map[string][]string) func(string) ([]string, error) {
	return func(pattern string) ([]string, error) {
		if result, ok := files[pattern]; ok {
			return result, nil
		}
		return nil, nil
	}
}

func mockScanner(fileContents map[string][]string) scanLineFn {
	return func(path string, process func(line string)) error {
		lines, ok := fileContents[path]
		if !ok {
			return errors.New("file not found: " + path)
		}
		for _, line := range lines {
			process(line)
		}
		return nil
	}
}

// --- specs ---

var _ = Describe("Azure/Functions/AnalyzeLogs", func() {
	var p AzureFunctionsAnalyzeLogs

	Describe("Identifier()", func() {
		It("should return correct identifier", func() {
			Expect(p.Identifier()).To(Equal(tasks.Identifier{
				Category:    "Azure",
				Subcategory: "Functions",
				Name:        "AnalyzeLogs",
			}))
		})
	})

	Describe("Dependencies()", func() {
		It("should depend on Azure/Functions/DetectFunctionApp", func() {
			Expect(p.Dependencies()).To(ConsistOf("Azure/Functions/DetectFunctionApp"))
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
				}
			})
			It("should return None", func() {
				Expect(result.Status).To(Equal(tasks.None))
			})
		})

		Context("when no log files are found", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					"Azure/Functions/DetectFunctionApp": {Status: tasks.Info},
				}
				p.logPathGlober = mockGlober(map[string][]string{})
				p.lineScanner = mockScanner(map[string][]string{})
			})
			It("should return None", func() {
				Expect(result.Status).To(Equal(tasks.None))
			})
		})

		Context("when logs contain no NR issues", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					"Azure/Functions/DetectFunctionApp": {Status: tasks.Info},
				}
				p.logPathGlober = mockGlober(map[string][]string{
					"/home/LogFiles/Application/*.txt": {"/home/LogFiles/Application/app.txt"},
				})
				p.lineScanner = mockScanner(map[string][]string{
					"/home/LogFiles/Application/app.txt": {
						"2026-01-01 INFO Function executed successfully",
						"2026-01-01 INFO Processed 100 requests",
					},
				})
			})
			It("should return Success", func() {
				Expect(result.Status).To(Equal(tasks.Success))
			})
		})

		Context("when logs contain NR errors", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					"Azure/Functions/DetectFunctionApp": {Status: tasks.Info},
				}
				p.logPathGlober = mockGlober(map[string][]string{
					"/home/LogFiles/Application/*.txt": {"/home/LogFiles/Application/app.txt"},
				})
				p.lineScanner = mockScanner(map[string][]string{
					"/home/LogFiles/Application/app.txt": {
						"2026-01-01 ERROR NewRelic error: Failed to connect to collector",
						"2026-01-01 INFO Some other message",
					},
				})
			})
			It("should return Failure", func() {
				Expect(result.Status).To(Equal(tasks.Failure))
			})
			It("should include error details in summary", func() {
				Expect(result.Summary).To(ContainSubstring("New Relic error"))
			})
			It("should populate NRErrors in the payload", func() {
				analysis, ok := result.Payload.(*LogAnalysisResult)
				Expect(ok).To(BeTrue())
				Expect(analysis.NRErrors).NotTo(BeEmpty())
			})
		})

		Context("when logs contain NR warnings but no errors", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					"Azure/Functions/DetectFunctionApp": {Status: tasks.Info},
				}
				p.logPathGlober = mockGlober(map[string][]string{
					"/home/LogFiles/Application/*.txt": {"/home/LogFiles/Application/app.txt"},
				})
				p.lineScanner = mockScanner(map[string][]string{
					"/home/LogFiles/Application/app.txt": {
						"2026-01-01 WARN NewRelic warning: deprecated configuration option",
					},
				})
			})
			It("should return Warning", func() {
				Expect(result.Status).To(Equal(tasks.Warning))
			})
		})

		Context("when a log file cannot be read", func() {
			BeforeEach(func() {
				options = tasks.Options{}
				upstream = map[string]tasks.Result{
					"Azure/Functions/DetectFunctionApp": {Status: tasks.Info},
				}
				p.logPathGlober = mockGlober(map[string][]string{
					"/home/LogFiles/Application/*.txt": {"/home/LogFiles/Application/unreadable.txt"},
				})
				// scanner returns error — task should skip gracefully
				p.lineScanner = mockScanner(map[string][]string{})
			})
			It("should not crash and return Success (no matches found)", func() {
				Expect(result.Status).To(Equal(tasks.Success))
			})
		})
	})

	Describe("truncate()", func() {
		It("leaves short strings unchanged", func() {
			Expect(truncate("hello", 10)).To(Equal("hello"))
		})
		It("truncates long strings", func() {
			Expect(truncate("hello world", 5)).To(Equal("hello..."))
		})
	})
})
