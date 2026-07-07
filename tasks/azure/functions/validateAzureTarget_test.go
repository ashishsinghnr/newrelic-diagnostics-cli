package functions

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("validateAzureTarget()", func() {
	Context("with valid names", func() {
		It("accepts typical function app and resource group names", func() {
			Expect(validateAzureTarget("my-func", "my-rg")).To(Succeed())
			Expect(validateAzureTarget("Func123", "rg")).To(Succeed())
			Expect(validateAzureTarget("a1", "rg_name.with(parens)")).To(Succeed())
		})
	})

	Context("with a functionName that would inject a CLI flag", func() {
		It("rejects a value beginning with '-' or '--'", func() {
			Expect(validateAzureTarget("--debug", "my-rg")).ToNot(Succeed())
			Expect(validateAzureTarget("-func", "my-rg")).ToNot(Succeed())
		})
	})

	Context("with a resourceGroup that would inject a CLI flag", func() {
		It("rejects a value beginning with '-' or '--'", func() {
			Expect(validateAzureTarget("my-func", "--query")).ToNot(Succeed())
			Expect(validateAzureTarget("my-func", "--subscription=evil")).ToNot(Succeed())
			Expect(validateAzureTarget("my-func", "-rg")).ToNot(Succeed())
		})
	})

	Context("with otherwise malformed values", func() {
		It("rejects empty, hostname-breaking, and out-of-range values", func() {
			Expect(validateAzureTarget("", "my-rg")).ToNot(Succeed())
			Expect(validateAzureTarget("my-func", "")).ToNot(Succeed())
			// A dot in the function name would break out of the .scm.azurewebsites.net label.
			Expect(validateAzureTarget("evil.com", "my-rg")).ToNot(Succeed())
			// Resource group names may not end with a period.
			Expect(validateAzureTarget("my-func", "rg.")).ToNot(Succeed())
		})
	})
})
