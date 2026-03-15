package discovery

import (
	"context"
	"fmt"
	"time"

	"fuse/internal/domain"
)

type Faker struct {
	NodesPerSwitch int
	GPUsPerNode    int
}

func NewFaker() *Faker {
	return &Faker{
		NodesPerSwitch: 4,
		GPUsPerNode:    8,
	}
}

func (f *Faker) Discover(_ context.Context) (Snapshot, error) {
	if f.NodesPerSwitch <= 0 {
		f.NodesPerSwitch = 4
	}
	if f.GPUsPerNode <= 0 {
		f.GPUsPerNode = 8
	}
	switches := []string{"leaf-01", "leaf-02"}
	var snapshot Snapshot
	for switchIdx, switchName := range switches {
		for nodeIdx := 0; nodeIdx < f.NodesPerSwitch; nodeIdx++ {
			nodeNumber := switchIdx*f.NodesPerSwitch + nodeIdx + 1
			nodeID := fmt.Sprintf("node-%02d", nodeNumber)
			nodeName := fmt.Sprintf("n%d", nodeNumber)
			node := domain.Node{
				ID:              nodeID,
				Name:            nodeName,
				SwitchName:      switchName,
				Rack:            fmt.Sprintf("rack-%02d", switchIdx+1),
				Health:          domain.HealthHealthy,
				DiscoverySource: domain.DiscoverySourceFaker,
				TotalGPUs:       f.GPUsPerNode,
				AllocatedGPUs:   0,
				FreeGPUs:        f.GPUsPerNode,
				ObservedState:   "idle",
			}
			snapshot.Nodes = append(snapshot.Nodes, node)
			for gpuIdx := 0; gpuIdx < f.GPUsPerNode; gpuIdx++ {
				snapshot.Devices = append(snapshot.Devices, domain.Device{
					ID:       fmt.Sprintf("%s-gpu-%d", nodeID, gpuIdx),
					NodeID:   nodeID,
					GPUIndex: gpuIdx,
					Vendor:   "nvidia",
					Model:    "H100",
					MemoryMB: 80 * 1024,
					Health:   domain.HealthHealthy,
					Benchmark: domain.Benchmark{
						GPUName:        "H100",
						MemoryMB:       80 * 1024,
						TemperatureC:   40 + gpuIdx%6,
						UtilizationPct: 0,
						MeasuredAt:     time.Now().UTC(),
					},
				})
			}
		}
	}
	for i, src := range snapshot.Nodes {
		for j := i + 1; j < len(snapshot.Nodes); j++ {
			dst := snapshot.Nodes[j]
			link := domain.FabricLink{
				SourceNodeID:  src.ID,
				TargetNodeID:  dst.ID,
				Bidirectional: true,
			}
			if src.SwitchName == dst.SwitchName {
				link.Tier = "same_switch"
				link.BandwidthGbps = 400
				link.LatencyClass = "low"
			} else {
				link.Tier = "cross_switch"
				link.BandwidthGbps = 100
				link.LatencyClass = "medium"
			}
			snapshot.Links = append(snapshot.Links, link)
		}
	}
	return snapshot, nil
}
