package planner

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"fuse/internal/domain"
)

type Planner struct{}

func New() *Planner {
	return &Planner{}
}

type Input struct {
	Job         domain.JobSpec
	Teams       []domain.Team
	Nodes       []domain.Node
	Devices     []domain.Device
	ActiveJobs  []domain.Job
	Allocations []domain.Allocation
}

type Output struct {
	Allocation domain.Allocation
	Why        domain.Why
}

func (p *Planner) Plan(_ context.Context, in Input) (Output, error) {
	usageByTeam := teamUsage(in.ActiveJobs)
	teamQuota := teamQuota(in.Teams, in.Job.Team)
	burst := teamBurstEnabled(in.Teams, in.Job.Team)
	overQuota := teamQuota > 0 && usageByTeam[in.Job.Team]+in.Job.GPUs > teamQuota
	if overQuota && !in.Job.Preemptable && !burst {
		return Output{
			Why: domain.Why{
				ReasonCode:  domain.ReasonQuotaExceeded,
				Summary:     "job exceeds team quota",
				Detail:      fmt.Sprintf("team %s currently uses %d GPUs and requested %d more, which exceeds quota %d", in.Job.Team, usageByTeam[in.Job.Team], in.Job.GPUs, teamQuota),
				Suggestions: []string{"retry as preemptable", "lower gpu request", "increase team quota", "enable burst for team"},
			},
		}, nil
	}
	allocatedDevices := allocatedDeviceSet(in.Allocations, in.ActiveJobs)
	availableByNode := map[string][]domain.Device{}
	nodesByID := map[string]domain.Node{}
	for _, node := range in.Nodes {
		nodesByID[node.ID] = node
	}
	for _, device := range in.Devices {
		if device.Health != domain.HealthHealthy {
			continue
		}
		if _, allocated := allocatedDevices[device.ID]; allocated {
			continue
		}
		availableByNode[device.NodeID] = append(availableByNode[device.NodeID], device)
	}
	type candidate struct {
		nodeIDs   []string
		deviceIDs []string
		score     int
		detail    string
	}
	var best *candidate
	if in.Job.TopologyHint == domain.TopologySameNode || in.Job.Type == domain.JobTypeFinetune {
		for _, node := range in.Nodes {
			devices := availableByNode[node.ID]
			if len(devices) < in.Job.GPUs {
				continue
			}
			sort.Slice(devices, func(i, j int) bool { return devices[i].GPUIndex < devices[j].GPUIndex })
			var deviceIDs []string
			for i := 0; i < in.Job.GPUs; i++ {
				deviceIDs = append(deviceIDs, devices[i].ID)
			}
			c := candidate{
				nodeIDs:   []string{node.ID},
				deviceIDs: deviceIDs,
				score:     1000 - len(devices),
				detail:    fmt.Sprintf("%d GPUs fit on single node %s", in.Job.GPUs, node.Name),
			}
			if best == nil || c.score > best.score {
				best = &c
			}
		}
		if best == nil && in.Job.TopologyHint == domain.TopologySameNode {
			return Output{
				Why: domain.Why{
					ReasonCode:  domain.ReasonTopologyUnsatisfied,
					Summary:     "job cannot fit on a single node",
					Detail:      fmt.Sprintf("requested %d GPUs with topology %s but no node has enough free healthy devices", in.Job.GPUs, in.Job.TopologyHint),
					Suggestions: []string{"reduce gpu request", "relax topology to same_switch or any"},
				},
			}, nil
		}
	}
	if best == nil {
		type switchBucket struct {
			nodeIDs   []string
			deviceIDs []string
			score     int
		}
		bySwitch := map[string]switchBucket{}
		switchMetadataAvailable := false
		for _, node := range in.Nodes {
			if strings.TrimSpace(node.SwitchName) == "" {
				continue
			}
			switchMetadataAvailable = true
			devices := availableByNode[node.ID]
			bucket := bySwitch[node.SwitchName]
			bucket.nodeIDs = append(bucket.nodeIDs, node.ID)
			for _, d := range devices {
				bucket.deviceIDs = append(bucket.deviceIDs, d.ID)
			}
			bySwitch[node.SwitchName] = bucket
		}
		for switchName, bucket := range bySwitch {
			if len(bucket.deviceIDs) < in.Job.GPUs {
				continue
			}
			c := candidate{
				nodeIDs:   uniqueStrings(bucket.nodeIDs),
				deviceIDs: bucket.deviceIDs[:in.Job.GPUs],
				score:     500 - len(bucket.nodeIDs),
				detail:    fmt.Sprintf("%d GPUs fit within switch %s", in.Job.GPUs, switchName),
			}
			if best == nil || c.score > best.score {
				best = &c
			}
		}
		if best == nil && in.Job.TopologyHint == domain.TopologySameSwitch && switchMetadataAvailable {
			return Output{
				Why: domain.Why{
					ReasonCode:  domain.ReasonTopologyUnsatisfied,
					Summary:     "job cannot fit on a single switch",
					Detail:      fmt.Sprintf("requested %d GPUs with topology %s but no switch has enough free healthy devices", in.Job.GPUs, in.Job.TopologyHint),
					Suggestions: []string{"relax topology to any", "reduce gpu request", "wait for more capacity"},
				},
			}, nil
		}
	}
	if best == nil {
		var deviceIDs []string
		nodeSet := map[string]struct{}{}
		for _, node := range in.Nodes {
			for _, device := range availableByNode[node.ID] {
				if len(deviceIDs) >= in.Job.GPUs {
					break
				}
				deviceIDs = append(deviceIDs, device.ID)
				nodeSet[node.ID] = struct{}{}
			}
		}
		if len(deviceIDs) < in.Job.GPUs {
			totalFree := 0
			for _, devices := range availableByNode {
				totalFree += len(devices)
			}
			return Output{
				Why: domain.Why{
					ReasonCode:  domain.ReasonInsufficientGPUs,
					Summary:     "not enough free GPUs",
					Detail:      fmt.Sprintf("requested %d GPUs but only %d healthy free devices are available", in.Job.GPUs, totalFree),
					Suggestions: []string{"wait for capacity", "reduce gpu request", "cancel or finish existing work"},
				},
			}, nil
		}
		best = &candidate{
			nodeIDs:   mapsKeys(nodeSet),
			deviceIDs: deviceIDs,
			score:     100,
			detail:    fmt.Sprintf("%d GPUs fit across %d nodes", in.Job.GPUs, len(nodeSet)),
		}
	}
	best.score += priorityBonus(in.Job.PriorityHint)
	constraints := []string{string(in.Job.TopologyHint)}
	if overQuota && burst {
		constraints = append(constraints, "burst")
	}
	why := domain.Why{
		JobID:        in.Job.ID,
		ReasonCode:   domain.ReasonScheduled,
		Summary:      "planner found a placement",
		Detail:       best.detail,
		Suggestions:  []string{"submit job through Slurm"},
		CurrentState: domain.JobStatePending,
	}
	return Output{
		Allocation: domain.Allocation{
			JobID:        in.Job.ID,
			NodeIDs:      uniqueStrings(best.nodeIDs),
			DeviceIDs:    uniqueStrings(best.deviceIDs),
			PlannerScore: best.score,
			Constraints:  constraints,
			CreatedAt:    time.Now().UTC(),
		},
		Why: why,
	}, nil
}

func teamUsage(jobs []domain.Job) map[string]int {
	out := map[string]int{}
	for _, job := range jobs {
		if job.State.Terminal() {
			continue
		}
		out[job.Team] += job.GPUs
	}
	return out
}

func teamQuota(teams []domain.Team, teamName string) int {
	for _, team := range teams {
		if team.Name == teamName {
			return team.QuotaGPUs
		}
	}
	return 0
}

func teamBurstEnabled(teams []domain.Team, name string) bool {
	for _, team := range teams {
		if team.Name == name {
			return team.BurstEnabled
		}
	}
	return false
}

func priorityBonus(hint string) int {
	switch hint {
	case domain.PriorityHigh:
		return 50
	case domain.PriorityLow:
		return -50
	default:
		return 0
	}
}

func allocatedDeviceSet(allocations []domain.Allocation, jobs []domain.Job) map[string]struct{} {
	activeJobs := map[string]struct{}{}
	for _, job := range jobs {
		if !job.State.Terminal() {
			activeJobs[job.ID] = struct{}{}
		}
	}
	out := map[string]struct{}{}
	for _, allocation := range allocations {
		if _, ok := activeJobs[allocation.JobID]; !ok {
			continue
		}
		for _, deviceID := range allocation.DeviceIDs {
			out[deviceID] = struct{}{}
		}
	}
	return out
}

func uniqueStrings(items []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, item := range items {
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func mapsKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func ExplainPending(job domain.Job) domain.Why {
	if job.ReasonCode == "" {
		job.ReasonCode = domain.ReasonUnknown
	}
	detail := strings.TrimSpace(job.ReasonDetail)
	if detail == "" {
		detail = job.ReasonSummary
	}
	return domain.Why{
		JobID:        job.ID,
		ReasonCode:   job.ReasonCode,
		Summary:      job.ReasonSummary,
		Detail:       detail,
		Suggestions:  job.Suggestions,
		SlurmJobID:   job.SlurmJobID,
		NodeList:     append([]string(nil), job.NodeList...),
		ExitCode:     job.ExitCode,
		RawState:     job.RawState,
		CurrentState: job.State,
	}
}
