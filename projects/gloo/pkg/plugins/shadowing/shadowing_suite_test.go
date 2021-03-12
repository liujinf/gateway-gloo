package shadowing_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	"github.com/solo-io/go-utils/testutils"
)

func TestTracing(t *testing.T) {
	testutils.RegisterCommonFailHandlers()
	RunSpecs(t, "Shadowing Suite")
}
