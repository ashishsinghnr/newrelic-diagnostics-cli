package functions

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("AzureFunctionsFetchAppSettings.ObfuscatePayload()", func() {
	var t AzureFunctionsFetchAppSettings

	It("masks non-New-Relic secrets: connection strings, storage keys, registry passwords", func() {
		in := map[string]string{
			"AzureWebJobsStorage":                      "DefaultEndpointsProtocol=https;AccountName=foo;AccountKey=abc123secret==;EndpointSuffix=core.windows.net",
			"WEBSITE_CONTENTAZUREFILECONNECTIONSTRING": "DefaultEndpointsProtocol=https;AccountName=foo;AccountKey=xyz==",
			"APPLICATIONINSIGHTS_CONNECTION_STRING":    "InstrumentationKey=00000000-0000-0000-0000-000000000000;IngestionEndpoint=https://x",
			"DOCKER_REGISTRY_SERVER_PASSWORD":          "sup3rSecretPass",
			"MyBackend":                                "Server=tcp:db;Password=hunter2;",
			"APPINSIGHTS_INSTRUMENTATIONKEY":           "00000000-0000-0000-0000-000000000000",
		}
		out, ok := t.ObfuscatePayload(in).(map[string]string)
		Expect(ok).To(BeTrue())

		Expect(out["AzureWebJobsStorage"]).To(Equal("****"))
		Expect(out["WEBSITE_CONTENTAZUREFILECONNECTIONSTRING"]).To(Equal("****"))
		Expect(out["APPLICATIONINSIGHTS_CONNECTION_STRING"]).To(Equal("****"))
		Expect(out["DOCKER_REGISTRY_SERVER_PASSWORD"]).To(Equal("****"))
		Expect(out["MyBackend"]).To(Equal("****"))
		Expect(out["APPINSIGHTS_INSTRUMENTATIONKEY"]).To(Equal("****"))
	})

	It("masks New Relic secrets using the existing key rules", func() {
		in := map[string]string{
			"NEW_RELIC_LICENSE_KEY": "1234567890abcdef1234567890abcdef12345678",
			"NEW_RELIC_API_KEY":     "NRAK-ABCDEFGHIJKLMNOP",
		}
		out := t.ObfuscatePayload(in).(map[string]string)
		Expect(out["NEW_RELIC_LICENSE_KEY"]).To(Equal("****"))
		// _KEY suffix keeps a short prefix per maskIfSensitive.
		Expect(out["NEW_RELIC_API_KEY"]).To(Equal("NRAK****"))
	})

	It("leaves non-sensitive diagnostic values readable", func() {
		in := map[string]string{
			"FUNCTIONS_WORKER_RUNTIME": "dotnet-isolated",
			"NEW_RELIC_APP_NAME":       "my-service",
			"CORECLR_ENABLE_PROFILING": "1",
			"WEBSITE_CONTENTSHARE":     "myfuncapp-content",
		}
		out := t.ObfuscatePayload(in).(map[string]string)
		Expect(out["FUNCTIONS_WORKER_RUNTIME"]).To(Equal("dotnet-isolated"))
		Expect(out["NEW_RELIC_APP_NAME"]).To(Equal("my-service"))
		Expect(out["CORECLR_ENABLE_PROFILING"]).To(Equal("1"))
		Expect(out["WEBSITE_CONTENTSHARE"]).To(Equal("myfuncapp-content"))
	})

	It("does not mutate the original in-memory payload used by downstream tasks", func() {
		in := map[string]string{"AzureWebJobsStorage": "AccountKey=secret=="}
		_ = t.ObfuscatePayload(in)
		Expect(in["AzureWebJobsStorage"]).To(Equal("AccountKey=secret=="))
	})

	It("returns the payload unchanged when it is not a string map", func() {
		Expect(t.ObfuscatePayload("not a map")).To(Equal("not a map"))
		Expect(t.ObfuscatePayload(nil)).To(BeNil())
	})
})
