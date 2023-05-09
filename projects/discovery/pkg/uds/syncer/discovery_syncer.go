package syncer

import (
	"context"
	"time"

	"github.com/solo-io/go-utils/contextutils"
	"github.com/solo-io/go-utils/hashutils"
	"go.uber.org/zap/zapcore"

	"github.com/solo-io/gloo/pkg/utils/syncutil"
	v1 "github.com/solo-io/gloo/projects/gloo/pkg/api/v1"
	"github.com/solo-io/gloo/projects/gloo/pkg/discovery"
)

type syncer struct {
	uds         *discovery.UpstreamDiscovery
	refreshRate time.Duration
}

func NewDiscoverySyncer(disc *discovery.UpstreamDiscovery, refreshRate time.Duration) v1.DiscoverySyncer {
	s := &syncer{
		uds:         disc,
		refreshRate: refreshRate,
	}
	return s
}

func (s *syncer) Sync(ctx context.Context, snap *v1.DiscoverySnapshot) error {
	ctx = contextutils.WithLogger(ctx, "syncer")
	logger := contextutils.LoggerFrom(ctx)
	snapHash := hashutils.MustHash(snap)
	logger.Infof("begin sync %v (%v upstreams)", snapHash, len(snap.Upstreams))
	defer logger.Infof("end sync %v", snapHash)

	// stringifying the snapshot may be an expensive operation, so we'd like to avoid building the large
	// string if we're not even going to log it anyway
	if contextutils.GetLogLevel() == zapcore.DebugLevel {
		logger.Debug(syncutil.StringifySnapshot(snap))
	}

	// kick the uds, ensure that desired upstreams are in sync
	return s.uds.Resync(ctx)
}
