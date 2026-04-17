package functions

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/newrelic/newrelic-diagnostics-cli/tasks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	testFuncName        = "my-func"
	testBearerToken     = "fake-token\n"
	headerContentType   = "Content-Type"
	mimeApplicationJSON = "application/json"
)

var _ = Describe("Azure/Functions/CollectLiveMemoryDump", func() {
	var p AzureFunctionsCollectLiveMemoryDump

	BeforeEach(func() {
		p = AzureFunctionsCollectLiveMemoryDump{}
	})

	Describe("Identifier()", func() {
		It("should return correct identifier", func() {
			Expect(p.Identifier()).To(Equal(tasks.Identifier{
				Category:    "Azure",
				Subcategory: "Functions",
				Name:        "CollectLiveMemoryDump",
			}))
		})
	})

	Describe("Dependencies()", func() {
		It("should depend on CollectProcessDetails and DownloadSiteDump", func() {
			Expect(p.Dependencies()).To(ConsistOf(
				"Azure/Functions/CollectProcessDetails",
				"Azure/Functions/DownloadSiteDump",
			))
		})
	})

	Describe("Execute()", func() {
		var (
			result    tasks.Result
			options   tasks.Options
			upstream  map[string]tasks.Result
		)

		JustBeforeEach(func() {
			result = p.Execute(options, upstream)
		})

		Context("when functionName or resourceGroup are not provided", func() {
			BeforeEach(func() {
				options = tasks.Options{Options: map[string]string{}}
				upstream = map[string]tasks.Result{}
			})
			It("should return None", func() {
				Expect(result.Status).To(Equal(tasks.None))
			})
		})

		Context("when user says no to the prompt (YesToAll=false, simulated via options)", func() {
			BeforeEach(func() {
				// We cannot easily test interactive stdin in unit tests.
				// This context verifies the None path when functionName is missing.
				options = tasks.Options{Options: map[string]string{
					"functionName":  "",
					"resourceGroup": "rg",
				}}
				upstream = map[string]tasks.Result{}
			})
			It("should return None", func() {
				Expect(result.Status).To(Equal(tasks.None))
			})
		})

		Context("when YesToAll and auth succeeds and Kudu returns process list + dump", func() {
			var (
				server    *httptest.Server
				outputDir string
			)

			BeforeEach(func() {
				var err error
				outputDir, err = os.MkdirTemp("", "nrdiag-memdump-test-*")
				Expect(err).NotTo(HaveOccurred())

				processes := []kuduProcess{
					{ID: 42, Name: "dotnet", Href: ""},
				}
				processJSON, _ := json.Marshal(processes)

				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/api/processes/" {
						w.Header().Set(headerContentType, mimeApplicationJSON)
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write(processJSON)
						return
					}
					// /api/processes/42/dump
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("fake-memory-dump-content"))
				}))

				p.cmdRunner = mockCmdRunner(
					map[string][]byte{"account": []byte(testBearerToken)},
					nil,
				)
				p.httpClient = &http.Client{
					Transport: rewriteHostTransport{
						inner:  server.Client().Transport,
						target: server.URL,
					},
				}

				options = tasks.Options{Options: map[string]string{
					"functionName":  testFuncName,
					"resourceGroup": "my-rg",
					"outputPath":    outputDir,
					"YesToAll":      "true", // skip interactive prompt
				}}
				upstream = map[string]tasks.Result{}
			})

			AfterEach(func() {
				server.Close()
				os.RemoveAll(outputDir)
			})

			It("should return Info", func() {
				Expect(result.Status).To(Equal(tasks.Info))
			})
			It("should include process name and PID in summary", func() {
				Expect(result.Summary).To(ContainSubstring("dotnet"))
				Expect(result.Summary).To(ContainSubstring("42"))
			})
			It("should create the dump file on disk", func() {
				dr, ok := result.Payload.(*MemoryDumpResult)
				Expect(ok).To(BeTrue())
				_, err := os.Stat(dr.OutputPath)
				Expect(err).NotTo(HaveOccurred())
			})
			It("should include the file in FilesToCopy", func() {
				Expect(result.FilesToCopy).To(HaveLen(1))
				Expect(result.FilesToCopy[0].Identifier).To(Equal("Azure/Functions/CollectLiveMemoryDump"))
			})
		})

		Context("when processID override is provided", func() {
			var (
				server    *httptest.Server
				outputDir string
			)

			BeforeEach(func() {
				var err error
				outputDir, err = os.MkdirTemp("", "nrdiag-memdump-pid-test-*")
				Expect(err).NotTo(HaveOccurred())

				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					// Should hit /api/processes/99/dump directly — no process list call.
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("fake-dump"))
				}))

				p.cmdRunner = mockCmdRunner(
					map[string][]byte{"account": []byte(testBearerToken)},
					nil,
				)
				p.httpClient = &http.Client{
					Transport: rewriteHostTransport{
						inner:  server.Client().Transport,
						target: server.URL,
					},
				}

				options = tasks.Options{Options: map[string]string{
					"functionName":  testFuncName,
					"resourceGroup": "my-rg",
					"processID":     "99",
					"outputPath":    outputDir,
					"YesToAll":      "true",
				}}
				upstream = map[string]tasks.Result{}
			})

			AfterEach(func() {
				server.Close()
				os.RemoveAll(outputDir)
			})

			It("should return Info", func() {
				Expect(result.Status).To(Equal(tasks.Info))
			})
			It("should include the specified PID in the summary", func() {
				Expect(result.Summary).To(ContainSubstring("99"))
			})
		})

		Context("when processID override is not a valid integer", func() {
			BeforeEach(func() {
				options = tasks.Options{Options: map[string]string{
					"functionName":  testFuncName,
					"resourceGroup": "my-rg",
					"processID":     "not-a-number",
					"YesToAll":      "true",
				}}
				upstream = map[string]tasks.Result{}
			})
			It("should return Error", func() {
				Expect(result.Status).To(Equal(tasks.Error))
			})
			It("should mention the invalid value", func() {
				Expect(result.Summary).To(ContainSubstring("not-a-number"))
			})
		})

		Context("when Kudu returns non-200 for dump", func() {
			var server *httptest.Server

			BeforeEach(func() {
				processes := []kuduProcess{{ID: 1, Name: "dotnet", Href: ""}}
				processJSON, _ := json.Marshal(processes)

				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/api/processes/" {
						w.Header().Set(headerContentType, mimeApplicationJSON)
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write(processJSON)
						return
					}
					w.WriteHeader(http.StatusForbidden)
				}))

				p.cmdRunner = mockCmdRunner(
					map[string][]byte{"account": []byte(testBearerToken)},
					nil,
				)
				p.httpClient = &http.Client{
					Transport: rewriteHostTransport{
						inner:  server.Client().Transport,
						target: server.URL,
					},
				}

				options = tasks.Options{Options: map[string]string{
					"functionName":  testFuncName,
					"resourceGroup": "my-rg",
					"YesToAll":      "true",
				}}
				upstream = map[string]tasks.Result{}
			})

			AfterEach(func() { server.Close() })

			It("should return Failure", func() {
				Expect(result.Status).To(Equal(tasks.Failure))
			})
		})
	})

	Describe("listProcesses()", func() {
		It("returns the process list from Kudu", func() {
			processes := []kuduProcess{
				{ID: 5, Name: "bash"},
				{ID: 42, Name: "dotnet"},
			}
			processJSON, _ := json.Marshal(processes)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set(headerContentType, mimeApplicationJSON)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(processJSON)
			}))
			defer server.Close()

			list, err := listProcesses(server.Client(), server.URL, "Bearer fake")
			Expect(err).NotTo(HaveOccurred())
			Expect(list).To(HaveLen(2))
			Expect(list[1].ID).To(Equal(42))
		})

		It("returns error when Kudu returns non-200", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			}))
			defer server.Close()

			_, err := listProcesses(server.Client(), server.URL, "Bearer bad")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("401"))
		})
	})

	Describe("autoSelectProcess()", func() {
		It("returns the first dotnet process", func() {
			processes := []kuduProcess{
				{ID: 5, Name: "bash"},
				{ID: 42, Name: "dotnet"},
			}
			pid, name, found := autoSelectProcess(processes)
			Expect(found).To(BeTrue())
			Expect(pid).To(Equal(42))
			Expect(name).To(Equal("dotnet"))
		})

		It("returns a node process", func() {
			processes := []kuduProcess{
				{ID: 5, Name: "bash"},
				{ID: 77, Name: "node"},
			}
			pid, name, found := autoSelectProcess(processes)
			Expect(found).To(BeTrue())
			Expect(pid).To(Equal(77))
			Expect(name).To(Equal("node"))
		})

		It("returns a python process", func() {
			processes := []kuduProcess{
				{ID: 5, Name: "bash"},
				{ID: 88, Name: "python3"},
			}
			pid, name, found := autoSelectProcess(processes)
			Expect(found).To(BeTrue())
			Expect(pid).To(Equal(88))
			Expect(name).To(Equal("python3"))
		})

		It("returns a java process", func() {
			processes := []kuduProcess{
				{ID: 5, Name: "bash"},
				{ID: 99, Name: "java"},
			}
			pid, name, found := autoSelectProcess(processes)
			Expect(found).To(BeTrue())
			Expect(pid).To(Equal(99))
			Expect(name).To(Equal("java"))
		})

		It("returns not-found when no known runtime process exists", func() {
			processes := []kuduProcess{
				{ID: 5, Name: "bash"},
				{ID: 6, Name: "sh"},
			}
			_, _, found := autoSelectProcess(processes)
			Expect(found).To(BeFalse())
		})
	})

	// ensure fmt is used
	var _ = fmt.Sprintf
})
