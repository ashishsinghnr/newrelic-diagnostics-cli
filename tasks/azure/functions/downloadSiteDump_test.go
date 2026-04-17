package functions

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	"github.com/newrelic/newrelic-diagnostics-cli/tasks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// mockCmdRunner returns a function that responds to az CLI commands with
// pre-configured output/error pairs keyed by the first argument after "az".
func mockCmdRunner(responses map[string][]byte, errors map[string]error) func(string, ...string) ([]byte, error) {
	return func(name string, args ...string) ([]byte, error) {
		// Build a simple key from the sub-command (args[0]).
		key := ""
		if len(args) > 0 {
			key = args[0]
		}
		if err, ok := errors[key]; ok && err != nil {
			return nil, err
		}
		if out, ok := responses[key]; ok {
			return out, nil
		}
		return nil, fmt.Errorf("unexpected command: %s %v", name, args)
	}
}

var _ = Describe("Azure/Functions/DownloadSiteDump", func() {
	var p AzureFunctionsDownloadSiteDump

	BeforeEach(func() {
		p = AzureFunctionsDownloadSiteDump{}
	})

	Describe("Identifier()", func() {
		It("should return correct identifier", func() {
			Expect(p.Identifier()).To(Equal(tasks.Identifier{
				Category:    "Azure",
				Subcategory: "Functions",
				Name:        "DownloadSiteDump",
			}))
		})
	})

	Describe("Dependencies()", func() {
		It("should return an empty dependency list", func() {
			Expect(p.Dependencies()).To(BeEmpty())
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
			It("should mention the required options in summary", func() {
				Expect(result.Summary).To(ContainSubstring("functionName"))
				Expect(result.Summary).To(ContainSubstring("resourceGroup"))
			})
		})

		Context("when az CLI bearer token succeeds and Kudu returns 200", func() {
			var (
				server    *httptest.Server
				outputDir string
			)

			BeforeEach(func() {
				var err error
				outputDir, err = os.MkdirTemp("", "nrdiag-test-*")
				Expect(err).NotTo(HaveOccurred())

				// Serve a fake zip body.
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					Expect(r.Header.Get("Authorization")).To(HavePrefix("Bearer "))
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("PK\x03\x04fake-zip-content"))
				}))

				p.cmdRunner = mockCmdRunner(
					map[string][]byte{"account": []byte("fake-bearer-token\n")},
					nil,
				)
				p.httpClient = server.Client()

				options = tasks.Options{Options: map[string]string{
					"functionName":  "my-func",
					"resourceGroup": "my-rg",
					"outputPath":    outputDir,
				}}
				upstream = map[string]tasks.Result{}

				// Point the task at the test server instead of *.scm.azurewebsites.net
				// by injecting a transport that rewrites the host.
				p.httpClient = &http.Client{
					Transport: rewriteHostTransport{
						inner:   server.Client().Transport,
						target:  server.URL,
					},
				}
			})

			AfterEach(func() {
				server.Close()
				os.RemoveAll(outputDir)
			})

			It("should return Info", func() {
				Expect(result.Status).To(Equal(tasks.Info))
			})
			It("should include the output path in the summary", func() {
				Expect(result.Summary).To(ContainSubstring("my-func-site-dump.zip"))
			})
			It("should populate the payload with SiteDumpResult", func() {
				dr, ok := result.Payload.(*SiteDumpResult)
				Expect(ok).To(BeTrue())
				Expect(dr.FunctionAppName).To(Equal("my-func"))
				Expect(dr.ResourceGroup).To(Equal("my-rg"))
				Expect(dr.OutputPath).To(ContainSubstring("my-func-site-dump.zip"))
			})
			It("should create the dump file on disk", func() {
				dr := result.Payload.(*SiteDumpResult)
				_, err := os.Stat(dr.OutputPath)
				Expect(err).NotTo(HaveOccurred())
			})
			It("should include the file in FilesToCopy", func() {
				Expect(result.FilesToCopy).To(HaveLen(1))
				Expect(result.FilesToCopy[0].Identifier).To(Equal("Azure/Functions/DownloadSiteDump"))
			})
		})

		Context("when bearer token fails but basic auth succeeds", func() {
			var (
				server    *httptest.Server
				outputDir string
			)

			BeforeEach(func() {
				var err error
				outputDir, err = os.MkdirTemp("", "nrdiag-test-*")
				Expect(err).NotTo(HaveOccurred())

				expectedBasic := "Basic " + base64.StdEncoding.EncodeToString([]byte("$my-func:secret"))

				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					Expect(r.Header.Get("Authorization")).To(Equal(expectedBasic))
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("PK\x03\x04fake-zip-content"))
				}))

				creds, _ := json.Marshal(publishingCredentials{
					PublishingUserName: "$my-func",
					PublishingPassword: "secret",
				})
				p.cmdRunner = mockCmdRunner(
					map[string][]byte{"functionapp": creds},
					map[string]error{"account": fmt.Errorf("az login required")},
				)
				p.httpClient = &http.Client{
					Transport: rewriteHostTransport{
						inner:  server.Client().Transport,
						target: server.URL,
					},
				}

				options = tasks.Options{Options: map[string]string{
					"functionName":  "my-func",
					"resourceGroup": "my-rg",
					"outputPath":    outputDir,
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
		})

		Context("when both auth methods fail", func() {
			BeforeEach(func() {
				p.cmdRunner = mockCmdRunner(
					nil,
					map[string]error{
						"account":     fmt.Errorf("not logged in"),
						"functionapp": fmt.Errorf("resource group not found"),
					},
				)

				options = tasks.Options{Options: map[string]string{
					"functionName":  "my-func",
					"resourceGroup": "my-rg",
				}}
				upstream = map[string]tasks.Result{}
			})

			It("should return Error", func() {
				Expect(result.Status).To(Equal(tasks.Error))
			})
			It("should mention authentication failure in summary", func() {
				Expect(result.Summary).To(ContainSubstring("authenticate"))
			})
		})

		Context("when Kudu returns a non-200 status", func() {
			var server *httptest.Server

			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusForbidden)
				}))

				p.cmdRunner = mockCmdRunner(
					map[string][]byte{"account": []byte("fake-token\n")},
					nil,
				)
				p.httpClient = &http.Client{
					Transport: rewriteHostTransport{
						inner:  server.Client().Transport,
						target: server.URL,
					},
				}

				options = tasks.Options{Options: map[string]string{
					"functionName":  "my-func",
					"resourceGroup": "my-rg",
				}}
				upstream = map[string]tasks.Result{}
			})

			AfterEach(func() { server.Close() })

			It("should return Failure", func() {
				Expect(result.Status).To(Equal(tasks.Failure))
			})
			It("should mention the HTTP status code", func() {
				Expect(result.Summary).To(ContainSubstring("403"))
			})
		})
	})

	Describe("getBearerToken()", func() {
		It("returns the token when az CLI succeeds", func() {
			runner := mockCmdRunner(
				map[string][]byte{"account": []byte("  my-token  \n")},
				nil,
			)
			token, err := getBearerToken(runner)
			Expect(err).NotTo(HaveOccurred())
			Expect(token).To(Equal("my-token"))
		})

		It("returns error when az CLI fails", func() {
			runner := mockCmdRunner(nil, map[string]error{"account": fmt.Errorf("not logged in")})
			_, err := getBearerToken(runner)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("getPublishingCredentials()", func() {
		It("returns credentials when az CLI succeeds", func() {
			creds, _ := json.Marshal(publishingCredentials{
				PublishingUserName: "$func",
				PublishingPassword: "pass123",
			})
			runner := mockCmdRunner(map[string][]byte{"functionapp": creds}, nil)
			u, p, err := getPublishingCredentials(runner, "func", "rg")
			Expect(err).NotTo(HaveOccurred())
			Expect(u).To(Equal("$func"))
			Expect(p).To(Equal("pass123"))
		})

		It("returns error when az CLI fails", func() {
			runner := mockCmdRunner(nil, map[string]error{"functionapp": fmt.Errorf("not found")})
			_, _, err := getPublishingCredentials(runner, "func", "rg")
			Expect(err).To(HaveOccurred())
		})
	})
})

// rewriteHostTransport redirects all requests to a test server URL while
// preserving the path and query string, so tests can intercept Kudu calls.
type rewriteHostTransport struct {
	inner  http.RoundTripper
	target string // e.g. "http://127.0.0.1:PORT"
}

func (r rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = "http"
	clone.URL.Host = strings.TrimPrefix(r.target, "http://")
	if r.inner == nil {
		return http.DefaultTransport.RoundTrip(clone)
	}
	// Ensure we don't send TLS to the test server.
	transport, ok := r.inner.(*http.Transport)
	if ok {
		plain := transport.Clone()
		plain.TLSClientConfig = nil
		return plain.RoundTrip(clone)
	}
	return http.DefaultTransport.RoundTrip(clone)
}

// ensure io import is used
var _ io.Reader = strings.NewReader("")
var _ filepath.WalkFunc = nil
