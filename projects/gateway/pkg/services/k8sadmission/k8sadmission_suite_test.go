package k8sadmission_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestK8sAdmission(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "K8sAdmission Suite")
}
