package functions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/newrelic/newrelic-diagnostics-cli/tasks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Azure/Functions/CollectProcessDetails", func() {
	var p AzureFunctionsCollectProcessDetails

	BeforeEach(func() {
		p = AzureFunctionsCollectProcessDetails{}
	})

	Describe("Identifier()", func() {
		It("should return correct identifier", func() {
			Expect(p.Identifier()).To(Equal(tasks.Identifier{
				Category:    "Azure",
				Subcategory: "Functions",
				Name:        "CollectProcessDetails",
			}))
		})
	})

	Describe("Dependencies()", func() {
		It("should depend on DownloadSiteDump", func() {
			Expect(p.Dependencies()).To(ConsistOf("Azure/Functions/DownloadSiteDump"))
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
			It("should return None", func() {
				Expect(result.Status).To(Equal(tasks.None))
			})
		})

		Context("when YesToAll and Kudu returns full process detail", func() {
			var (
				server    *httptest.Server
				outputDir string
			)

			BeforeEach(func() {
				var err error
				outputDir, err = os.MkdirTemp("", "nrdiag-procdetail-test-*")
				Expect(err).NotTo(HaveOccurred())

				processList := []kuduProcess{
					{ID: 42, Name: "dotnet", Href: ""},
				}
				processListJSON, _ := json.Marshal(processList)

				detail := kuduProcessDetail{
					ID:          42,
					Name:        "dotnet",
					HandleCount: 100,
					ModuleCount: 50,
					ThreadCount: 10,
					StartTime:   "2024-01-01T00:00:00Z",
					Modules:     json.RawMessage(`[{"name":"System.dll"}]`),
					Threads:     json.RawMessage(`[{"id":1}]`),
					OpenFileHandles: json.RawMessage(`["/tmp/foo"]`),
					EnvironmentVariables: json.RawMessage(`{"NEW_RELIC_APP_NAME":"my-app"}`),
				}
				detailJSON, _ := json.Marshal(detail)

				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set(headerContentType, mimeApplicationJSON)
					w.WriteHeader(http.StatusOK)
					if r.URL.Path == "/api/processes/" {
						_, _ = w.Write(processListJSON)
						return
					}
					// /api/processes/42
					_, _ = w.Write(detailJSON)
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
			It("should mention the process name and PID in the summary", func() {
				Expect(result.Summary).To(ContainSubstring("dotnet"))
				Expect(result.Summary).To(ContainSubstring("42"))
			})
			It("should create 5 output files", func() {
				dr, ok := result.Payload.(*ProcessDetailsResult)
				Expect(ok).To(BeTrue())
				Expect(dr.Files).To(HaveLen(5))
				for _, f := range dr.Files {
					_, err := os.Stat(f)
					Expect(err).NotTo(HaveOccurred())
				}
			})
			It("should include all 5 files in FilesToCopy", func() {
				Expect(result.FilesToCopy).To(HaveLen(5))
				for _, e := range result.FilesToCopy {
					Expect(e.Identifier).To(Equal("Azure/Functions/CollectProcessDetails"))
				}
			})
			It("should name files with the function name and PID", func() {
				dr, ok := result.Payload.(*ProcessDetailsResult)
				Expect(ok).To(BeTrue())
				Expect(dr.Files[0]).To(ContainSubstring(testFuncName))
				Expect(dr.Files[0]).To(ContainSubstring("pid42"))
			})
		})

		Context("when Kudu returns non-200 for process detail", func() {
			var server *httptest.Server

			BeforeEach(func() {
				processList := []kuduProcess{{ID: 1, Name: "dotnet", Href: ""}}
				processListJSON, _ := json.Marshal(processList)

				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/api/processes/" {
						w.Header().Set(headerContentType, mimeApplicationJSON)
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write(processListJSON)
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

			It("should return Error", func() {
				Expect(result.Status).To(Equal(tasks.Error))
			})
		})
	})

	Describe("fetchProcessDetail()", func() {
		It("parses the Kudu response into a kuduProcessDetail", func() {
			detail := kuduProcessDetail{
				ID:          7,
				Name:        "dotnet",
				HandleCount: 42,
				Modules:     json.RawMessage(`[{"name":"clr.dll"}]`),
				EnvironmentVariables: json.RawMessage(`{"PATH":"/usr/bin"}`),
			}
			detailJSON, _ := json.Marshal(detail)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set(headerContentType, mimeApplicationJSON)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(detailJSON)
			}))
			defer server.Close()

			got, err := fetchProcessDetail(server.Client(), server.URL, "Bearer fake", 7)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ID).To(Equal(7))
			Expect(got.Name).To(Equal("dotnet"))
			Expect(got.HandleCount).To(Equal(42))
		})

		It("returns error when Kudu returns non-200", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			}))
			defer server.Close()

			_, err := fetchProcessDetail(server.Client(), server.URL, "Bearer bad", 1)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("401"))
		})
	})

	Describe("saveProcessDetails()", func() {
		It("writes 5 files with correct name suffixes", func() {
			outputDir, err := os.MkdirTemp("", "nrdiag-savedetail-*")
			Expect(err).NotTo(HaveOccurred())
			defer os.RemoveAll(outputDir)

			detail := &kuduProcessDetail{
				ID:                   99,
				Name:                 "dotnet",
				Modules:              json.RawMessage(`[]`),
				Threads:              json.RawMessage(`[]`),
				OpenFileHandles:      json.RawMessage(`[]`),
				EnvironmentVariables: json.RawMessage(`{}`),
			}

			files, err := saveProcessDetails(detail, outputDir, "my-func")
			Expect(err).NotTo(HaveOccurred())
			Expect(files).To(HaveLen(5))

			suffixes := []string{"general", "modules", "handles", "threads", "environment"}
			for i, s := range suffixes {
				Expect(files[i]).To(ContainSubstring(s))
				_, statErr := os.Stat(files[i])
				Expect(statErr).NotTo(HaveOccurred())
			}
		})
	})

	Describe("buildGeneralSection()", func() {
		It("contains all scalar fields", func() {
			d := &kuduProcessDetail{
				ID:          5,
				Name:        "dotnet",
				HandleCount: 10,
				WorkingSet:  1024,
			}
			section := buildGeneralSection(d)
			Expect(section["id"]).To(Equal(5))
			Expect(section["name"]).To(Equal("dotnet"))
			Expect(section["handle_count"]).To(Equal(10))
			Expect(section["working_set_bytes"]).To(BeEquivalentTo(1024))
		})
	})
})
