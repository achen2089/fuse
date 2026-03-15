package planner

import (
	"context"
	"testing"

	"fuse/internal/domain"
)

func TestPlanPrefersSingleNodeForFinetune(t *testing.T) {
	p := New()
	job := domain.JobSpec{
		ID:              "job-1",
		Name:            "job-1",
		Team:            "default",
		Type:            domain.JobTypeFinetune,
		CommandOrRecipe: "python train.py",
		GPUs:            4,
		TopologyHint:    domain.TopologySameNode,
	}
	out, err := p.Plan(context.Background(), Input{
		Job:   job,
		Teams: []domain.Team{{Name: "default", QuotaGPUs: 8}},
		Nodes: []domain.Node{
			{ID: "n1", Name: "n1", SwitchName: "leaf-01"},
			{ID: "n2", Name: "n2", SwitchName: "leaf-01"},
		},
		Devices: buildDevices("n1", 8, false),
	})
	if err != nil {
		t.Fatalf("plan failed: %v", err)
	}
	if len(out.Allocation.NodeIDs) != 1 || out.Allocation.NodeIDs[0] != "n1" {
		t.Fatalf("expected single-node allocation on n1, got %#v", out.Allocation.NodeIDs)
	}
}

func TestPlanReturnsQuotaExceeded(t *testing.T) {
	p := New()
	job := domain.JobSpec{
		ID:              "job-1",
		Name:            "job-1",
		Team:            "vision",
		Type:            domain.JobTypeRun,
		CommandOrRecipe: "python train.py",
		GPUs:            4,
	}
	out, err := p.Plan(context.Background(), Input{
		Job:        job,
		Teams:      []domain.Team{{Name: "vision", QuotaGPUs: 2}},
		Nodes:      []domain.Node{{ID: "n1", Name: "n1", SwitchName: "leaf-01"}},
		Devices:    buildDevices("n1", 8, false),
		ActiveJobs: []domain.Job{{ID: "other", Team: "vision", GPUs: 0, State: domain.JobStateRunning}},
	})
	if err != nil {
		t.Fatalf("plan failed: %v", err)
	}
	if out.Why.ReasonCode != domain.ReasonQuotaExceeded {
		t.Fatalf("expected quota exceeded, got %s", out.Why.ReasonCode)
	}
}

func buildDevices(nodeID string, count int, allocated bool) []domain.Device {
	devices := make([]domain.Device, 0, count)
	for i := 0; i < count; i++ {
		devices = append(devices, domain.Device{
			ID:       nodeID + "-gpu-" + string(rune('a'+i)),
			NodeID:   nodeID,
			GPUIndex: i,
			Health:   domain.HealthHealthy,
		})
	}
	return devices
}
