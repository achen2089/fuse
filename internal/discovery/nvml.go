package discovery

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"fuse/internal/domain"
)

type NVML struct{}

func NewNVML() *NVML {
	return &NVML{}
}

func (n *NVML) Discover(ctx context.Context) (Snapshot, error) {
	cmd := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu=name,memory.total,temperature.gpu,utilization.gpu", "--format=csv,noheader,nounits")
	output, err := cmd.Output()
	if err != nil {
		return Snapshot{}, err
	}
	lines := strings.Split(strings.TrimSpace(string(bytes.TrimSpace(output))), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return Snapshot{}, fmt.Errorf("nvidia-smi returned no GPUs")
	}
	node := domain.Node{
		ID:              "real-node-01",
		Name:            "real-gpu-node",
		SwitchName:      "real-switch",
		Rack:            "real-rack",
		Health:          domain.HealthHealthy,
		DiscoverySource: domain.DiscoverySourceNVML,
		Real:            true,
	}
	snapshot := Snapshot{
		Nodes: []domain.Node{node},
	}
	for idx, line := range lines {
		parts := strings.Split(line, ",")
		if len(parts) < 4 {
			continue
		}
		model := strings.TrimSpace(parts[0])
		memoryMB := parseInt64(strings.TrimSpace(parts[1]))
		temp := int(parseInt64(strings.TrimSpace(parts[2])))
		util := int(parseInt64(strings.TrimSpace(parts[3])))
		snapshot.Devices = append(snapshot.Devices, domain.Device{
			ID:       fmt.Sprintf("%s-gpu-%d", node.ID, idx),
			NodeID:   node.ID,
			GPUIndex: idx,
			Vendor:   "nvidia",
			Model:    model,
			MemoryMB: memoryMB,
			Health:   domain.HealthHealthy,
			Real:     true,
			Benchmark: domain.Benchmark{
				GPUName:        model,
				MemoryMB:       memoryMB,
				TemperatureC:   temp,
				UtilizationPct: util,
				MeasuredAt:     time.Now().UTC(),
			},
		})
	}
	node.TotalGPUs = len(snapshot.Devices)
	node.FreeGPUs = len(snapshot.Devices)
	node.ObservedState = "idle"
	snapshot.Nodes[0] = node
	return snapshot, nil
}

func parseInt64(raw string) int64 {
	var v int64
	fmt.Sscanf(raw, "%d", &v)
	return v
}
