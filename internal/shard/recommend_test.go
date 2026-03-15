package shard

import (
	"fmt"
	"testing"

	"fuse/internal/domain"
)

func TestRecommendPrefersDataParallelForSmallModels(t *testing.T) {
	nodes := []domain.Node{{ID: "n1", TotalGPUs: 8}, {ID: "n2", TotalGPUs: 8}}
	devices := make([]domain.Device, 0, 16)
	for _, nodeID := range []string{"n1", "n2"} {
		for gpu := 0; gpu < 8; gpu++ {
			devices = append(devices, domain.Device{
				ID:       fmt.Sprintf("%s-gpu-%d", nodeID, gpu),
				NodeID:   nodeID,
				GPUIndex: gpu,
				MemoryMB: 183359,
			})
		}
	}

	plan, err := Recommend(domain.ShardRequest{Model: "llama-7b", GPUs: 8}, nodes, devices)
	if err != nil {
		t.Fatalf("recommend shard: %v", err)
	}
	if !plan.Fits {
		t.Fatalf("expected plan to fit: %#v", plan)
	}
	if plan.TensorParallel != 1 || plan.DataParallel != 8 {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	if plan.TopologyHint != domain.TopologySameNode {
		t.Fatalf("expected same-node topology, got %s", plan.TopologyHint)
	}
}

func TestRecommendPrefersPipelineOverCrossNodeTensorParallel(t *testing.T) {
	nodes := []domain.Node{
		{ID: "n1", TotalGPUs: 8, SwitchName: "leaf-01"},
		{ID: "n2", TotalGPUs: 8, SwitchName: "leaf-01"},
	}
	devices := make([]domain.Device, 0, 16)
	for _, nodeID := range []string{"n1", "n2"} {
		for gpu := 0; gpu < 8; gpu++ {
			devices = append(devices, domain.Device{
				ID:       fmt.Sprintf("%s-gpu-%d", nodeID, gpu),
				NodeID:   nodeID,
				GPUIndex: gpu,
				MemoryMB: 183359,
			})
		}
	}

	plan, err := Recommend(domain.ShardRequest{Model: "llama-70b", GPUs: 16}, nodes, devices)
	if err != nil {
		t.Fatalf("recommend shard: %v", err)
	}
	if !plan.Fits {
		t.Fatalf("expected plan to fit: %#v", plan)
	}
	if plan.TensorParallel != 8 || plan.PipelineParallel != 2 || plan.DataParallel != 1 {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	if plan.TopologyHint != domain.TopologySameSwitch {
		t.Fatalf("expected same-switch topology, got %s", plan.TopologyHint)
	}
}

func TestRecommendReportsUnsupportedModel(t *testing.T) {
	_, err := Recommend(domain.ShardRequest{Model: "unknown-1b", GPUs: 4}, nil, nil)
	if err == nil {
		t.Fatal("expected unsupported model error")
	}
}

func TestRecommendSupportsHugeModelsWithTPAndPP(t *testing.T) {
	nodes := []domain.Node{{ID: "n1", TotalGPUs: 8}, {ID: "n2", TotalGPUs: 8}}
	devices := make([]domain.Device, 0, 16)
	for _, nodeID := range []string{"n1", "n2"} {
		for gpu := 0; gpu < 8; gpu++ {
			devices = append(devices, domain.Device{
				ID:       fmt.Sprintf("%s-gpu-%d", nodeID, gpu),
				NodeID:   nodeID,
				GPUIndex: gpu,
				MemoryMB: 183359,
			})
		}
	}

	plan, err := Recommend(domain.ShardRequest{Model: "llama-405b", GPUs: 16}, nodes, devices)
	if err != nil {
		t.Fatalf("recommend shard: %v", err)
	}
	if !plan.Fits {
		t.Fatalf("expected plan to fit: %#v", plan)
	}
	if plan.TensorParallel != 8 || plan.PipelineParallel != 2 {
		t.Fatalf("unexpected plan: %#v", plan)
	}
}
