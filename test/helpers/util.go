package helpers

import (
	"context"
	"time"

	. "github.com/onsi/gomega"
	"github.com/solo-io/solo-kit/pkg/api/v1/clients"
	"github.com/solo-io/solo-kit/pkg/api/v1/resources"
	"github.com/solo-io/solo-kit/pkg/api/v1/resources/core"
	skerrors "github.com/solo-io/solo-kit/pkg/errors"
)

// PatchResource mutates an existing persisted resource, retrying if a resourceVersionError is encountered
// The mutator method must return the full object that will be persisted, any side effects from the mutator will be ignored
func PatchResource(ctx context.Context, resourceRef *core.ResourceRef, mutator func(resource resources.Resource) resources.Resource, client clients.ResourceClient) error {
	return PatchResourceWithOffset(1, ctx, resourceRef, mutator, client)
}

// PatchResourceWithOffset mutates an existing persisted resource, retrying if a resourceVersionError is encountered
// The mutator method must return the full object that will be persisted, any side effects from the mutator will be ignored
func PatchResourceWithOffset(offset int, ctx context.Context, resourceRef *core.ResourceRef, mutator func(resource resources.Resource) resources.Resource, client clients.ResourceClient) error {
	// There is a potential bug in our resource writing implementation that leads to test flakes
	// https://github.com/solo-io/gloo/issues/7044
	// This is a temporary solution to ensure that tests do not flake

	var patchErr error

	EventuallyWithOffset(offset+1, func(g Gomega) {
		resource, err := client.Read(resourceRef.GetNamespace(), resourceRef.GetName(), clients.ReadOpts{Ctx: ctx})
		g.Expect(err).NotTo(HaveOccurred())
		resourceVersion := resource.GetMetadata().GetResourceVersion()

		mutatedResource := mutator(resource)
		mutatedResource.GetMetadata().ResourceVersion = resourceVersion

		_, patchErr = client.Write(mutatedResource, clients.WriteOpts{Ctx: ctx, OverwriteExisting: true})
		g.Expect(skerrors.IsResourceVersion(patchErr)).To(BeFalse())
	}, time.Second*5, time.Second).ShouldNot(HaveOccurred())

	return patchErr
}
