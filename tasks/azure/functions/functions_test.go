package functions

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAzureFunctions(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Azure/Functions/* test suite")
}
