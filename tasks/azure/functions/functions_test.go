package functions

import (
	"testing"

	"github.com/newrelic/newrelic-diagnostics-cli/tasks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAzureFunctions(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Azure/Functions/* test suite")
}

var _ = Describe("RegisterWith()", func() {
	It("registers exactly 12 tasks", func() {
		var registered []tasks.Task
		RegisterWith(func(t tasks.Task, _ bool) {
			registered = append(registered, t)
		})
		Expect(registered).To(HaveLen(12))
	})
})
