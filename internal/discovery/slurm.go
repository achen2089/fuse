package discovery

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"fuse/internal/domain"
)

type slurmRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

type Slurm struct {
	runner     slurmRunner
	remoteHost string
}

func NewSlurm(remoteHost string) *Slurm {
	return &Slurm{
		runner:     execRunner{},
		remoteHost: remoteHost,
	}
}

func (s *Slurm) Discover(ctx context.Context) (Snapshot, error) {
	output, err := s.run(ctx, "scontrol", "show", "nodes", "-o")
	if err != nil {
		return Snapshot{}, fmt.Errorf("slurm discovery failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return parseSlurmSnapshot(string(output))
}

func (s *Slurm) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if s.remoteHost == "" {
		return s.runner.Run(ctx, name, args...)
	}
	sshArgs := append([]string{s.remoteHost, name}, args...)
	return s.runner.Run(ctx, "ssh", sshArgs...)
}

var (
	slurmFieldStartPattern = regexp.MustCompile(`(?:^|\s)([A-Za-z_][A-Za-z0-9_]*)=`)
	slurmGPUCountPatterns  = []*regexp.Regexp{
		regexp.MustCompile(`(?:^|,)gres/gpu=(\d+)`),
		regexp.MustCompile(`gpu:(\d+)`),
	}
)

func parseSlurmSnapshot(raw string) (Snapshot, error) {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	snapshot := Snapshot{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		node, devices, ok, err := parseSlurmNodeLine(line)
		if err != nil {
			return Snapshot{}, err
		}
		if !ok {
			continue
		}
		snapshot.Nodes = append(snapshot.Nodes, node)
		snapshot.Devices = append(snapshot.Devices, devices...)
	}
	sort.Slice(snapshot.Nodes, func(i, j int) bool { return snapshot.Nodes[i].Name < snapshot.Nodes[j].Name })
	sort.Slice(snapshot.Devices, func(i, j int) bool {
		if snapshot.Devices[i].NodeID == snapshot.Devices[j].NodeID {
			return snapshot.Devices[i].GPUIndex < snapshot.Devices[j].GPUIndex
		}
		return snapshot.Devices[i].NodeID < snapshot.Devices[j].NodeID
	})
	if len(snapshot.Nodes) == 0 {
		return Snapshot{}, fmt.Errorf("slurm discovery returned no GPU nodes")
	}
	return snapshot, nil
}

func parseSlurmNodeLine(line string) (domain.Node, []domain.Device, bool, error) {
	fields := parseSlurmFields(line)
	nodeName := strings.TrimSpace(firstNonEmpty(fields["NodeHostName"], fields["NodeName"]))
	if nodeName == "" {
		return domain.Node{}, nil, false, nil
	}
	gpuCount := parseGPUCount(fields)
	if gpuCount <= 0 {
		return domain.Node{}, nil, false, nil
	}
	health := slurmStateToHealth(fields["State"])
	allocatedGPUs := extractGPUCount(fields["AllocTRES"])
	model, vendor, memoryMB := slurmGPUProfile(firstNonEmpty(fields["ActiveFeatures"], fields["AvailableFeatures"]))
	freeGPUs := gpuCount - allocatedGPUs
	if freeGPUs < 0 {
		freeGPUs = 0
	}
	node := domain.Node{
		ID:              nodeName,
		Name:            nodeName,
		SwitchName:      "",
		Rack:            "",
		Health:          health,
		DiscoverySource: domain.DiscoverySourceSlurm,
		TotalGPUs:       gpuCount,
		AllocatedGPUs:   allocatedGPUs,
		FreeGPUs:        freeGPUs,
		ObservedState:   strings.TrimSpace(fields["State"]),
		Real:            true,
	}
	devices := make([]domain.Device, 0, gpuCount)
	deviceHealth := domain.HealthHealthy
	if health != domain.HealthHealthy {
		deviceHealth = health
	}
	for idx := 0; idx < gpuCount; idx++ {
		devices = append(devices, domain.Device{
			ID:       fmt.Sprintf("%s-gpu-%d", node.ID, idx),
			NodeID:   node.ID,
			GPUIndex: idx,
			Vendor:   vendor,
			Model:    model,
			MemoryMB: memoryMB,
			Health:   deviceHealth,
			Real:     true,
		})
	}
	return node, devices, true, nil
}

func parseSlurmFields(line string) map[string]string {
	matches := slurmFieldStartPattern.FindAllStringSubmatchIndex(line, -1)
	fields := make(map[string]string, len(matches))
	for idx, match := range matches {
		if len(match) < 4 {
			continue
		}
		key := line[match[2]:match[3]]
		valueStart := match[1]
		valueEnd := len(line)
		if idx+1 < len(matches) {
			valueEnd = matches[idx+1][0]
		}
		fields[key] = strings.TrimSpace(line[valueStart:valueEnd])
	}
	return fields
}

func parseGPUCount(fields map[string]string) int {
	for _, raw := range []string{fields["CfgTRES"], fields["AllocTRES"], fields["Gres"]} {
		if count := extractGPUCount(raw); count > 0 {
			return count
		}
	}
	return 0
}

func extractGPUCount(raw string) int {
	for _, pattern := range slurmGPUCountPatterns {
		match := pattern.FindStringSubmatch(raw)
		if len(match) != 2 {
			continue
		}
		count, err := strconv.Atoi(match[1])
		if err == nil {
			return count
		}
	}
	return 0
}

func slurmStateToHealth(raw string) domain.HealthStatus {
	state := strings.ToUpper(raw)
	switch {
	case strings.Contains(state, "DOWN"), strings.Contains(state, "FAIL"), strings.Contains(state, "NOT_RESPONDING"):
		return domain.HealthOffline
	case strings.Contains(state, "DRAIN"), strings.Contains(state, "MAINT"), strings.Contains(state, "POWER"):
		return domain.HealthDegraded
	default:
		return domain.HealthHealthy
	}
}

func slurmGPUProfile(features string) (model, vendor string, memoryMB int64) {
	features = strings.ToLower(features)
	switch {
	case strings.Contains(features, "nvidia_b200"):
		return "NVIDIA B200", "nvidia", 183359
	case strings.Contains(features, "nvidia_h100"):
		return "NVIDIA H100", "nvidia", 80 * 1024
	case strings.Contains(features, "nvidia"):
		return "NVIDIA GPU", "nvidia", 0
	case strings.Contains(features, "amd"):
		return "AMD GPU", "amd", 0
	default:
		return "GPU", "", 0
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
