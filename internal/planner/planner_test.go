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

func TestPlanFallsBackWhenSwitchMetadataIsMissing(t *testing.T) {
	p := New()
	job := domain.JobSpec{
		ID:              "job-1",
		Name:            "job-1",
		Team:            "default",
		Type:            domain.JobTypeTrain,
		CommandOrRecipe: "python train.py",
		GPUs:            16,
		TopologyHint:    domain.TopologySameSwitch,
	}
	out, err := p.Plan(context.Background(), Input{
		Job:   job,
		Teams: []domain.Team{{Name: "default", QuotaGPUs: 32}},
		Nodes: []domain.Node{
			{ID: "n1", Name: "n1"},
			{ID: "n2", Name: "n2"},
		},
		Devices: append(buildDevices("n1", 8, false), buildDevices("n2", 8, false)...),
	})
	if err != nil {
		t.Fatalf("plan failed: %v", err)
	}
	if out.Allocation.JobID != "job-1" {
		t.Fatalf("allocation job id = %q, want job-1", out.Allocation.JobID)
	}
	if len(out.Allocation.NodeIDs) != 2 {
		t.Fatalf("expected cross-node fallback allocation, got %#v", out.Allocation.NodeIDs)
	}
	if out.Why.ReasonCode != domain.ReasonScheduled {
		t.Fatalf("reason = %s, want %s", out.Why.ReasonCode, domain.ReasonScheduled)
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
