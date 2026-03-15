package discovery

import (
	"context"

	"fuse/internal/domain"
)

type Snapshot struct {
	Nodes   []domain.Node
	Devices []domain.Device
	Links   []domain.FabricLink
}

type Discoverer interface {
	Discover(ctx context.Context) (Snapshot, error)
}
