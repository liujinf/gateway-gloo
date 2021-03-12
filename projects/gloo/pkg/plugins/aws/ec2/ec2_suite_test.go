package ec2

import (
	"testing"

	. "github.com/onsi/ginkgo"
	"github.com/solo-io/go-utils/testutils"
)

func TestEc2(t *testing.T) {
	testutils.RegisterCommonFailHandlers()
	RunSpecs(t, "EC2 Suite")
}
