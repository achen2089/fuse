package discovery

import (
	"strings"
	"testing"

	"fuse/internal/domain"
)

func TestParseSlurmNodeLineBuildsRealGPUNode(t *testing.T) {
	line := "NodeName=us-west-a2-gpu-011 Arch=x86_64 AvailableFeatures=nvidia_b200,nvidia_gpu,gpu_node ActiveFeatures=nvidia_b200,nvidia_gpu,gpu_node Gres=gpu:8(S:0-1) NodeHostName=us-west-a2-gpu-011 OS=Linux 6.8.0 RealMemory=2043912 State=MIXED+DYNAMIC_NORM CfgTRES=cpu=192,mem=2043912M,billing=192,gres/gpu=8 AllocTRES=cpu=140,mem=1176G,gres/gpu=8"
	node, devices, ok, err := parseSlurmNodeLine(line)
	if err != nil {
		t.Fatalf("parse node line: %v", err)
	}
	if !ok {
		t.Fatal("expected GPU node to be included")
	}
	if node.Name != "us-west-a2-gpu-011" {
		t.Fatalf("unexpected node name: %q", node.Name)
	}
	if node.DiscoverySource != domain.DiscoverySourceSlurm || !node.Real {
		t.Fatalf("unexpected node flags: %#v", node)
	}
	if node.SwitchName != "" {
		t.Fatalf("expected unknown switch to stay empty, got %q", node.SwitchName)
	}
	if node.Health != domain.HealthHealthy {
		t.Fatalf("unexpected node health: %s", node.Health)
	}
	if node.TotalGPUs != 8 || node.AllocatedGPUs != 8 || node.FreeGPUs != 0 {
		t.Fatalf("unexpected observed gpu counts: %#v", node)
	}
	if len(devices) != 8 {
		t.Fatalf("expected 8 devices, got %d", len(devices))
	}
	if devices[0].Model != "NVIDIA B200" || devices[0].MemoryMB != 183359 {
		t.Fatalf("unexpected device profile: %#v", devices[0])
	}
}

func TestParseSlurmNodeLineMapsDrainedStateToDegraded(t *testing.T) {
	line := "NodeName=us-west-a2-gpu-099 AvailableFeatures=nvidia_b200 Gres=gpu:8 State=DRAINED CfgTRES=cpu=192,mem=2043912M,billing=192,gres/gpu=8"
	node, devices, ok, err := parseSlurmNodeLine(line)
	if err != nil {
		t.Fatalf("parse node line: %v", err)
	}
	if !ok {
		t.Fatal("expected GPU node to be included")
	}
	if node.Health != domain.HealthDegraded {
		t.Fatalf("unexpected node health: %s", node.Health)
	}
	if node.AllocatedGPUs != 0 || node.FreeGPUs != 8 {
		t.Fatalf("unexpected gpu counts on drained node: %#v", node)
	}
	for _, device := range devices {
		if device.Health != domain.HealthDegraded {
			t.Fatalf("expected degraded device health, got %#v", device)
		}
	}
}

func TestParseSlurmSnapshotSkipsNonGPUEntries(t *testing.T) {
	raw := strings.Join([]string{
		"NodeName=cpu-only-001 State=IDLE CfgTRES=cpu=32,mem=128000M,billing=32",
		"NodeName=us-west-a2-gpu-001 ActiveFeatures=nvidia_b200 Gres=gpu:8 State=IDLE CfgTRES=cpu=192,mem=2043912M,billing=192,gres/gpu=8",
	}, "\n")
	snapshot, err := parseSlurmSnapshot(raw)
	if err != nil {
		t.Fatalf("parse snapshot: %v", err)
	}
	if len(snapshot.Nodes) != 1 {
		t.Fatalf("expected 1 GPU node, got %d", len(snapshot.Nodes))
	}
	if len(snapshot.Devices) != 8 {
		t.Fatalf("expected 8 devices, got %d", len(snapshot.Devices))
	}
}
