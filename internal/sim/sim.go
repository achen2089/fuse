package sim

import (
	"context"
	"fmt"
	"time"

	"fuse/internal/domain"
	"fuse/internal/planner"
)

type Simulator struct {
	planner *planner.Planner
}

func New(p *planner.Planner) *Simulator {
	return &Simulator{planner: p}
}

type Snapshot struct {
	Teams       []domain.Team
	Nodes       []domain.Node
	Devices     []domain.Device
	Jobs        []domain.Job
	Allocations []domain.Allocation
}

func (s *Simulator) Run(ctx context.Context, snapshot Snapshot, req domain.SimulationRequest) (domain.SimulationResult, error) {
	result := domain.SimulationResult{
		ID:        fmt.Sprintf("sim-%d", time.Now().UnixNano()),
		Action:    req.Action,
		CreatedAt: time.Now().UTC(),
	}
	switch req.Action {
	case domain.SimulationKillNode:
		if req.NodeID == "" {
			return result, fmt.Errorf("node_id is required")
		}
		var remainingNodes []domain.Node
		var remainingDevices []domain.Device
		for _, node := range snapshot.Nodes {
			if node.ID == req.NodeID {
				continue
			}
			remainingNodes = append(remainingNodes, node)
		}
		for _, device := range snapshot.Devices {
			if device.NodeID == req.NodeID {
				continue
			}
			remainingDevices = append(remainingDevices, device)
		}
		result.AffectedJobs = affectedJobs(req.NodeID, snapshot.Allocations)
		for _, jobID := range result.AffectedJobs {
			job, ok := findJob(snapshot.Jobs, jobID)
			if !ok {
				continue
			}
			out, err := s.planner.Plan(ctx, planner.Input{
				Job:         jobSpecFromJob(job),
				Teams:       snapshot.Teams,
				Nodes:       remainingNodes,
				Devices:     remainingDevices,
				ActiveJobs:  dropJob(snapshot.Jobs, jobID),
				Allocations: dropAllocation(snapshot.Allocations, jobID),
			})
			if err == nil && out.Why.ReasonCode == domain.ReasonScheduled {
				result.RecoveredJobs = append(result.RecoveredJobs, jobID)
			} else {
				result.FailedJobs = append(result.FailedJobs, jobID)
			}
		}
		result.Summary = fmt.Sprintf("Node %s removed from cluster. %d jobs affected, %d recoverable, %d blocked.", req.NodeID, len(result.AffectedJobs), len(result.RecoveredJobs), len(result.FailedJobs))
	case domain.SimulationAddNode:
		if req.AddNodes <= 0 {
			return result, fmt.Errorf("add_nodes must be > 0")
		}
		switchName := req.SwitchName
		if switchName == "" {
			switchName = "leaf-sim-01"
		}
		baseIdx := len(snapshot.Nodes) + 1
		gpusPerNode := 8
		for i := 0; i < req.AddNodes; i++ {
			nodeNumber := baseIdx + i
			nodeID := fmt.Sprintf("sim-node-%02d", nodeNumber)
			nodeName := fmt.Sprintf("sn%d", nodeNumber)
			node := domain.Node{
				ID:              nodeID,
				Name:            nodeName,
				SwitchName:      switchName,
				Rack:            fmt.Sprintf("rack-sim-%02d", (i/4)+1),
				Health:          domain.HealthHealthy,
				DiscoverySource: domain.DiscoverySourceFaker,
				TotalGPUs:       gpusPerNode,
				FreeGPUs:        gpusPerNode,
				ObservedState:   "idle",
			}
			snapshot.Nodes = append(snapshot.Nodes, node)
			for gpuIdx := 0; gpuIdx < gpusPerNode; gpuIdx++ {
				snapshot.Devices = append(snapshot.Devices, domain.Device{
					ID:       fmt.Sprintf("%s-gpu-%d", nodeID, gpuIdx),
					NodeID:   nodeID,
					GPUIndex: gpuIdx,
					Vendor:   "nvidia",
					Model:    "H100",
					MemoryMB: 80 * 1024,
					Health:   domain.HealthHealthy,
				})
			}
		}
		// Re-plan all PENDING jobs against the expanded cluster
		for _, job := range snapshot.Jobs {
			if job.State == domain.JobStatePending {
				result.AffectedJobs = append(result.AffectedJobs, job.ID)
				out, err := s.planner.Plan(ctx, planner.Input{
					Job:         jobSpecFromJob(job),
					Teams:       snapshot.Teams,
					Nodes:       snapshot.Nodes,
					Devices:     snapshot.Devices,
					ActiveJobs:  dropJob(snapshot.Jobs, job.ID),
					Allocations: dropAllocation(snapshot.Allocations, job.ID),
				})
				if err == nil && out.Why.ReasonCode == domain.ReasonScheduled {
					result.RecoveredJobs = append(result.RecoveredJobs, job.ID)
				} else {
					result.FailedJobs = append(result.FailedJobs, job.ID)
				}
			}
		}
		result.Summary = fmt.Sprintf("Added %d simulated nodes (%d GPUs) on switch %s. %d pending jobs re-evaluated, %d now schedulable.",
			req.AddNodes, req.AddNodes*gpusPerNode, switchName, len(result.AffectedJobs), len(result.RecoveredJobs))
	case domain.SimulationSubmit:
		if req.SubmitSpec == nil {
			return result, fmt.Errorf("submit_spec is required")
		}
		out, err := s.planner.Plan(ctx, planner.Input{
			Job:         *req.SubmitSpec,
			Teams:       snapshot.Teams,
			Nodes:       snapshot.Nodes,
			Devices:     snapshot.Devices,
			ActiveJobs:  snapshot.Jobs,
			Allocations: snapshot.Allocations,
		})
		if err != nil {
			return result, err
		}
		result.Summary = out.Why.Detail
	default:
		return result, fmt.Errorf("unsupported simulation action %q", req.Action)
	}
	return result, nil
}

func affectedJobs(nodeID string, allocations []domain.Allocation) []string {
	set := map[string]struct{}{}
	for _, allocation := range allocations {
		for _, id := range allocation.NodeIDs {
			if id == nodeID {
				set[allocation.JobID] = struct{}{}
			}
		}
	}
	var out []string
	for jobID := range set {
		out = append(out, jobID)
	}
	return out
}

func findJob(jobs []domain.Job, jobID string) (domain.Job, bool) {
	for _, job := range jobs {
		if job.ID == jobID {
			return job, true
		}
	}
	return domain.Job{}, false
}

func dropJob(jobs []domain.Job, jobID string) []domain.Job {
	var out []domain.Job
	for _, job := range jobs {
		if job.ID == jobID {
			continue
		}
		out = append(out, job)
	}
	return out
}

func dropAllocation(allocations []domain.Allocation, jobID string) []domain.Allocation {
	var out []domain.Allocation
	for _, allocation := range allocations {
		if allocation.JobID == jobID {
			continue
		}
		out = append(out, allocation)
	}
	return out
}

func jobSpecFromJob(job domain.Job) domain.JobSpec {
	return domain.JobSpec{
		ID:              job.ID,
		Name:            job.Name,
		Team:            job.Team,
		Type:            job.Type,
		CommandOrRecipe: job.CommandOrRecipe,
		Workdir:         job.Workdir,
		Env:             job.Env,
		GPUs:            job.GPUs,
		CPUs:            job.CPUs,
		MemoryMB:        job.MemoryMB,
		Walltime:        job.Walltime,
		CheckpointMode:  job.CheckpointMode,
		CheckpointDir:   job.CheckpointDir,
		ResumeCommand:   job.ResumeCommand,
		Preemptable:     job.Preemptable,
		PriorityHint:    job.PriorityHint,
		TopologyHint:    job.TopologyHint,
		ArtifactsDir:    job.ArtifactsDir,
	}
}
