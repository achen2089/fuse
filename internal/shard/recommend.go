package shard

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"fuse/internal/domain"
)

type modelProfile struct {
	Name     string
	Aliases  []string
	WeightGB float64
	Layers   int
	Heads    int
}

var profiles = []modelProfile{
	{Name: "llama-7b", Aliases: []string{"meta-llama/llama-2-7b-hf", "llama2-7b", "llama-2-7b"}, WeightGB: 14, Layers: 32, Heads: 32},
	{Name: "mistral-7b", Aliases: []string{"mistralai/mistral-7b", "mistral7b"}, WeightGB: 14, Layers: 32, Heads: 32},
	{Name: "llama-70b", Aliases: []string{"llama70b", "meta-llama/llama-70b", "llama-2-70b"}, WeightGB: 140, Layers: 80, Heads: 64},
	{Name: "mixtral-8x7b", Aliases: []string{"mixtral", "mixtral8x7b"}, WeightGB: 94, Layers: 32, Heads: 32},
	{Name: "llama-405b", Aliases: []string{"llama405b", "meta-llama/llama-405b"}, WeightGB: 810, Layers: 126, Heads: 128},
}

func Recommend(req domain.ShardRequest, nodes []domain.Node, devices []domain.Device) (domain.ShardPlan, error) {
	profile, ok := lookupModel(req.Model)
	if !ok {
		return domain.ShardPlan{}, fmt.Errorf("unsupported model %q (supported: %s)", req.Model, strings.Join(SupportedModels(), ", "))
	}
	method := strings.ToLower(strings.TrimSpace(req.Method))
	if method == "" {
		method = "full"
	}
	switch method {
	case "full", "lora", "inference":
	default:
		return domain.ShardPlan{}, fmt.Errorf("unsupported method %q (supported: full, lora, inference)", req.Method)
	}

	gpusPerNode := detectGPUsPerNode(nodes, devices)
	if gpusPerNode <= 0 {
		gpusPerNode = 8
	}
	if req.GPUs <= 0 {
		if req.Nodes > 0 {
			req.GPUs = req.Nodes * gpusPerNode
		} else {
			return domain.ShardPlan{}, fmt.Errorf("gpus must be greater than zero")
		}
	}
	deviceMemoryGB := detectDeviceMemoryGB(devices)
	if deviceMemoryGB <= 0 {
		deviceMemoryGB = 80
	}
	nodesNeeded := req.Nodes
	if nodesNeeded <= 0 {
		nodesNeeded = maxInt(1, int(math.Ceil(float64(req.GPUs)/float64(gpusPerNode))))
	}

	type candidate struct {
		tp       int
		pp       int
		dp       int
		memGB    float64
		score    float64
		detail   string
		topology domain.TopologyHint
	}

	var best *candidate
	for _, tp := range divisors(profile.Heads) {
		if tp <= 0 || tp > req.GPUs {
			continue
		}
		for _, pp := range divisors(profile.Layers) {
			if pp <= 0 || tp*pp > req.GPUs || req.GPUs%(tp*pp) != 0 {
				continue
			}
			dp := req.GPUs / (tp * pp)
			memGB := estimatePerGPUMemoryGB(profile.WeightGB, tp*pp, method)
			if memGB > deviceMemoryGB*0.85 {
				continue
			}
			topology := recommendedTopology(tp, pp, dp, req.GPUs, nodesNeeded, gpusPerNode, nodes)
			score := scoreCandidate(profile, tp, pp, dp, memGB, deviceMemoryGB, gpusPerNode, req.GPUs, nodesNeeded)
			detail := fmt.Sprintf("TP=%d, PP=%d, DP=%d keeps %.1f GB/GPU under %.1f GB device memory", tp, pp, dp, memGB, deviceMemoryGB)
			if best == nil || score > best.score {
				best = &candidate{
					tp:       tp,
					pp:       pp,
					dp:       dp,
					memGB:    memGB,
					score:    score,
					detail:   detail,
					topology: topology,
				}
			}
		}
	}

	if best == nil {
		needed := minimumShardsToFit(profile.WeightGB, deviceMemoryGB, method)
		suggestions := []string{
			fmt.Sprintf("increase the shard factor to at least %d total shards", needed),
			"use --method lora for a lighter training footprint",
			"request a model with fewer weights",
		}
		return domain.ShardPlan{
			Model:          profile.Name,
			Method:         method,
			GPUs:           req.GPUs,
			Nodes:          nodesNeeded,
			GPUsPerNode:    gpusPerNode,
			DeviceMemoryGB: round1(deviceMemoryGB),
			WeightGB:       profile.WeightGB,
			Fits:           false,
			TopologyHint:   domain.TopologyAny,
			Summary:        "requested slice cannot hold the model",
			Detail:         fmt.Sprintf("%s with method %s needs more than %.1f GB/GPU on %d GPUs", profile.Name, method, deviceMemoryGB, req.GPUs),
			Suggestions:    suggestions,
		}, nil
	}

	suggestions := []string{
		fmt.Sprintf("prefer %s placement for this layout", best.topology),
		fmt.Sprintf("start with TP=%d, PP=%d, DP=%d", best.tp, best.pp, best.dp),
	}
	if req.GPUs > gpusPerNode && best.tp > gpusPerNode {
		suggestions = append(suggestions, "avoid cross-node tensor parallel if you can add more pipeline stages")
	}
	if best.dp > 1 {
		suggestions = append(suggestions, "treat the extra ranks as data-parallel replicas")
	}

	return domain.ShardPlan{
		Model:                   profile.Name,
		Method:                  method,
		GPUs:                    req.GPUs,
		Nodes:                   nodesNeeded,
		GPUsPerNode:             gpusPerNode,
		DeviceMemoryGB:          round1(deviceMemoryGB),
		WeightGB:                profile.WeightGB,
		TensorParallel:          best.tp,
		PipelineParallel:        best.pp,
		DataParallel:            best.dp,
		PerGPUWeightGB:          round1(profile.WeightGB / float64(best.tp*best.pp)),
		EstimatedPerGPUMemoryGB: round1(best.memGB),
		Fits:                    true,
		TopologyHint:            best.topology,
		Summary:                 fmt.Sprintf("%s recommends TP=%d PP=%d DP=%d on %d GPUs", profile.Name, best.tp, best.pp, best.dp, req.GPUs),
		Detail:                  best.detail,
		Suggestions:             suggestions,
	}, nil
}

func SupportedModels() []string {
	names := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		names = append(names, profile.Name)
	}
	sort.Strings(names)
	return names
}

func lookupModel(raw string) (modelProfile, bool) {
	value := strings.ToLower(strings.TrimSpace(raw))
	for _, profile := range profiles {
		if value == profile.Name {
			return profile, true
		}
		for _, alias := range profile.Aliases {
			if value == strings.ToLower(alias) {
				return profile, true
			}
		}
	}
	return modelProfile{}, false
}

func detectGPUsPerNode(nodes []domain.Node, devices []domain.Device) int {
	maxSeen := 0
	for _, node := range nodes {
		if node.TotalGPUs > maxSeen {
			maxSeen = node.TotalGPUs
		}
	}
	if maxSeen > 0 {
		return maxSeen
	}
	counts := map[string]int{}
	for _, device := range devices {
		counts[device.NodeID]++
		if counts[device.NodeID] > maxSeen {
			maxSeen = counts[device.NodeID]
		}
	}
	return maxSeen
}

func detectDeviceMemoryGB(devices []domain.Device) float64 {
	var maxMB int64
	for _, device := range devices {
		if device.MemoryMB > maxMB {
			maxMB = device.MemoryMB
		}
	}
	if maxMB == 0 {
		return 0
	}
	return float64(maxMB) / 1024.0
}

func estimatePerGPUMemoryGB(weightGB float64, shardFactor int, method string) float64 {
	if shardFactor <= 0 {
		shardFactor = 1
	}
	shardedWeights := weightGB / float64(shardFactor)
	switch method {
	case "lora":
		return shardedWeights + 5
	case "inference":
		return (weightGB*1.15)/float64(shardFactor) + 2
	default:
		return (weightGB*1.85)/float64(shardFactor) + 3
	}
}

func minimumShardsToFit(weightGB, deviceMemoryGB float64, method string) int {
	for shards := 1; shards <= 1024; shards++ {
		if estimatePerGPUMemoryGB(weightGB, shards, method) <= deviceMemoryGB*0.85 {
			return shards
		}
	}
	return 1024
}

func recommendedTopology(tp, pp, dp, gpus, nodesNeeded, gpusPerNode int, nodes []domain.Node) domain.TopologyHint {
	if nodesNeeded <= 1 || gpus <= gpusPerNode {
		return domain.TopologySameNode
	}
	if tp <= gpusPerNode && pp > 1 {
		if switchCanHold(nodes, nodesNeeded) {
			return domain.TopologySameSwitch
		}
	}
	return domain.TopologyAny
}

func switchCanHold(nodes []domain.Node, nodesNeeded int) bool {
	if nodesNeeded <= 1 {
		return true
	}
	switchCounts := map[string]int{}
	for _, node := range nodes {
		if strings.TrimSpace(node.SwitchName) == "" {
			continue
		}
		switchCounts[node.SwitchName]++
		if switchCounts[node.SwitchName] >= nodesNeeded {
			return true
		}
	}
	return false
}

func scoreCandidate(profile modelProfile, tp, pp, dp int, memGB, deviceMemoryGB float64, gpusPerNode, totalGPUs, nodesNeeded int) float64 {
	score := 0.0
	util := memGB / deviceMemoryGB
	score -= math.Abs(util-0.55) * 40
	if tp <= gpusPerNode {
		score += 80
	} else {
		score -= 240
	}
	targetTP, targetPP, targetDP := targetPattern(profile, totalGPUs, gpusPerNode)
	score -= float64(absInt(tp-targetTP) * 18)
	score -= float64(absInt(pp-targetPP) * 30)
	score -= float64(absInt(dp-targetDP) * 12)

	if totalGPUs > gpusPerNode && pp > 1 && tp <= gpusPerNode {
		score += 45
	}
	if pp > targetPP*2 {
		score -= float64(pp-targetPP*2) * 30
	}
	if nodesNeeded > 0 && pp > nodesNeeded*2 {
		score -= float64(pp-nodesNeeded*2) * 15
	}
	if profile.WeightGB < 30 {
		score += float64(dp*24 - tp*6 - maxInt(pp-1, 0)*20)
	} else if profile.WeightGB < 100 {
		score += float64(tp*6 + dp*5 - maxInt(pp-1, 0)*8)
	} else {
		score += float64(tp*10 + pp*2 + minInt(dp, 2)*2 - maxInt(dp-1, 0)*8)
	}
	if pp > 1 && profile.Layers%pp == 0 {
		score += 10
	}
	if tp > totalGPUs/2 && tp > gpusPerNode {
		score -= 25
	}
	return score
}

func targetPattern(profile modelProfile, totalGPUs, gpusPerNode int) (int, int, int) {
	if totalGPUs <= 0 {
		return 1, 1, 1
	}
	if profile.WeightGB < 30 {
		return 1, 1, totalGPUs
	}

	targetTP := maxDivisorAtMost(profile.Heads, minInt(gpusPerNode, totalGPUs))
	if targetTP <= 0 {
		targetTP = minInt(gpusPerNode, totalGPUs)
	}
	targetPP := 1
	if totalGPUs > targetTP {
		switch {
		case profile.WeightGB >= 300:
			targetPP = minInt(4, maxInt(2, totalGPUs/(targetTP*2)))
		default:
			targetPP = 2
		}
	}
	for targetPP > 1 && totalGPUs%(targetTP*targetPP) != 0 {
		targetPP--
	}
	targetDP := maxInt(1, totalGPUs/(targetTP*targetPP))
	return targetTP, maxInt(1, targetPP), targetDP
}

func divisors(n int) []int {
	if n <= 0 {
		return nil
	}
	values := make([]int, 0, n)
	for i := 1; i <= n; i++ {
		if n%i == 0 {
			values = append(values, i)
		}
	}
	return values
}

func round1(v float64) float64 {
	return math.Round(v*10) / 10
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func maxDivisorAtMost(n, limit int) int {
	best := 0
	for _, value := range divisors(n) {
		if value <= limit && value > best {
			best = value
		}
	}
	return best
}
