package proxyprotocol_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestProxyProtocol(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Proxy Protocol Suite")
}
