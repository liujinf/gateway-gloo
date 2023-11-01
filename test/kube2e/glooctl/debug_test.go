package glooctl_test

import (
	"os"
	"path/filepath"

	"github.com/solo-io/gloo/test/kube2e"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Debug", Ordered, func() {

	// These tests formerly lived at: https://github.com/solo-io/gloo/blob/063dbf3ba7b7666d0111741c083b197364b14716/projects/gloo/cli/pkg/cmd/debug
	// They were migrated to this package since they depend on a k8s cluster

	BeforeAll(func() {
		// Check that everything is OK
		kube2e.GlooctlCheckEventuallyHealthy(1, testHelper, "90s")
	})

	Context("Logs", func() {

		Context("stdout", func() {

			It("should not crash", func() {
				_, err := GlooctlOut("debug", "logs", "-n", testHelper.InstallNamespace)
				Expect(err).NotTo(HaveOccurred())
			})

		})

		Context("file", func() {

			var (
				tmpDir string
			)

			BeforeEach(func() {
				var err error
				tmpDir, err = os.MkdirTemp("", "testDir")
				Expect(err).NotTo(HaveOccurred())
			})

			AfterEach(func() {
				_ = os.RemoveAll(tmpDir)
			})

			It("should create a tar file at location specified in --file when --zip is enabled", func() {
				outputFile := filepath.Join(tmpDir, "log.tgz")

				_, err := GlooctlOut("debug", "logs", "-n", testHelper.InstallNamespace, "--file", outputFile, "--zip", "true")
				Expect(err).NotTo(HaveOccurred(), "glooctl command should have succeeded")

				_, err = os.Stat(outputFile)
				Expect(err).NotTo(HaveOccurred(), "Output file should have been generated")
			})

			It("should create a text file at location specified in --file when --zip is not enabled", func() {
				outputFile := filepath.Join(tmpDir, "log.txt")

				_, err := GlooctlOut("debug", "logs", "-n", testHelper.InstallNamespace, "--file", outputFile, "--zip", "false")
				Expect(err).NotTo(HaveOccurred(), "glooctl command should have succeeded")

				_, err = os.Stat(outputFile)
				Expect(err).NotTo(HaveOccurred(), "Output file should have been generated")
			})
		})

	})

})
