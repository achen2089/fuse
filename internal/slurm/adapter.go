package slurm

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fuse/internal/domain"
)

type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

type Adapter struct {
	runner     Runner
	remoteHost string
	now        func() time.Time
}

func New(runner Runner, remoteHost string) *Adapter {
	if runner == nil {
		runner = ExecRunner{}
	}
	return &Adapter{
		runner:     runner,
		remoteHost: remoteHost,
		now:        func() time.Time { return time.Now().UTC() },
	}
}

type SubmitResult struct {
	SlurmJobID string
	ScriptPath string
}

type JobStatus struct {
	SlurmJobID string
	State      string
	NodeList   []string
	ExitCode   *int
	StartedAt  time.Time
	FinishedAt time.Time
}

type TopologyProbeMode string

const (
	TopologyProbeEphemeral  TopologyProbeMode = "ephemeral"
	TopologyProbeAllocation TopologyProbeMode = "allocation"
)

func (a *Adapter) Submit(ctx context.Context, spec domain.JobSpec, renderDir string) (SubmitResult, error) {
	if renderDir == "" {
		return SubmitResult{}, fmt.Errorf("render_dir is required")
	}
	if err := os.MkdirAll(renderDir, 0o755); err != nil {
		return SubmitResult{}, err
	}
	scriptPath := filepath.Join(renderDir, fmt.Sprintf("%s.sbatch", spec.Name))
	if err := os.WriteFile(scriptPath, []byte(a.RenderScript(spec)), 0o644); err != nil {
		return SubmitResult{}, err
	}
	output, err := a.runSubmit(ctx, scriptPath)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("sbatch failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	jobID := strings.TrimSpace(string(output))
	if jobID == "" {
		return SubmitResult{}, fmt.Errorf("sbatch returned no job id")
	}
	if idx := strings.Index(jobID, ";"); idx > 0 {
		jobID = jobID[:idx]
	}
	return SubmitResult{SlurmJobID: jobID, ScriptPath: scriptPath}, nil
}

func (a *Adapter) RenderScript(spec domain.JobSpec) string {
	var envLines []string
	keys := make([]string, 0, len(spec.Env))
	for key := range spec.Env {
		keys = append(keys, key)
	}
	sortStrings(keys)
	for _, key := range keys {
		envLines = append(envLines, fmt.Sprintf("export %s=%q", key, spec.Env[key]))
	}
	envLines = append(envLines,
		fmt.Sprintf("export FUSE_JOB_ID=%q", spec.ID),
		fmt.Sprintf("export FUSE_JOB_TYPE=%q", spec.Type),
		fmt.Sprintf("export FUSE_CHECKPOINT_DIR=%q", spec.CheckpointDir),
	)
	outputPath := filepath.Join(spec.ArtifactsDir, fmt.Sprintf("%s-%%j.out", spec.Name))
	errorPath := filepath.Join(spec.ArtifactsDir, fmt.Sprintf("%s-%%j.err", spec.Name))
	var buf bytes.Buffer
	buf.WriteString("#!/bin/bash\n")
	buf.WriteString(fmt.Sprintf("#SBATCH --job-name=%s\n", spec.Name))
	buf.WriteString(fmt.Sprintf("#SBATCH --gres=gpu:%d\n", spec.GPUs))
	if spec.ContainerImage != "" {
		buf.WriteString(fmt.Sprintf("#SBATCH --container-image=%s\n", spec.ContainerImage))
	}
	if len(spec.ContainerMounts) > 0 {
		buf.WriteString(fmt.Sprintf("#SBATCH --container-mounts=%s\n", strings.Join(spec.ContainerMounts, ",")))
	}
	if spec.ContainerWorkdir != "" {
		buf.WriteString(fmt.Sprintf("#SBATCH --container-workdir=%s\n", spec.ContainerWorkdir))
	}
	if spec.ContainerMountHome {
		buf.WriteString("#SBATCH --container-mount-home\n")
	}
	if spec.CPUs > 0 {
		buf.WriteString(fmt.Sprintf("#SBATCH --cpus-per-task=%d\n", spec.CPUs))
	}
	if spec.MemoryMB > 0 {
		buf.WriteString(fmt.Sprintf("#SBATCH --mem=%dM\n", spec.MemoryMB))
	}
	if spec.Walltime != "" {
		buf.WriteString(fmt.Sprintf("#SBATCH --time=%s\n", spec.Walltime))
	}
	buf.WriteString(fmt.Sprintf("#SBATCH --output=%s\n", outputPath))
	buf.WriteString(fmt.Sprintf("#SBATCH --error=%s\n\n", errorPath))
	if spec.Workdir != "" {
		buf.WriteString(fmt.Sprintf("cd %q\n", spec.Workdir))
	}
	if spec.CheckpointDir != "" {
		buf.WriteString(fmt.Sprintf("mkdir -p %q\n", spec.CheckpointDir))
	}
	if spec.ArtifactsDir != "" {
		buf.WriteString(fmt.Sprintf("mkdir -p %q\n", spec.ArtifactsDir))
	}
	for _, line := range envLines {
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	buf.WriteByte('\n')
	buf.WriteString(spec.CommandOrRecipe)
	buf.WriteByte('\n')
	return buf.String()
}

func (a *Adapter) Cancel(ctx context.Context, slurmJobID string) error {
	output, err := a.run(ctx, "scancel", slurmJobID)
	if err != nil {
		return fmt.Errorf("scancel failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (a *Adapter) ReadLog(ctx context.Context, logPath string, tailLines int) ([]byte, error) {
	if logPath == "" {
		return nil, fmt.Errorf("log path is required")
	}
	if a.remoteHost == "" {
		return readLocalLog(logPath, tailLines)
	}
	command := fmt.Sprintf("cat -- %s", shellQuote(logPath))
	if tailLines > 0 {
		command = fmt.Sprintf("tail -n %d -- %s", tailLines, shellQuote(logPath))
	}
	output, err := a.runShell(ctx, command)
	if err != nil {
		return nil, fmt.Errorf("remote log read failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func (a *Adapter) Storage(ctx context.Context, target string) (domain.StorageStatus, error) {
	if strings.TrimSpace(target) == "" {
		return domain.StorageStatus{}, fmt.Errorf("storage path is required")
	}
	output, err := a.run(ctx, "df", "-B1", "-P", "--", target)
	if err != nil {
		return domain.StorageStatus{}, fmt.Errorf("df failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	filesystems, err := parseDFOutput(string(output))
	if err != nil {
		return domain.StorageStatus{}, err
	}
	return domain.StorageStatus{
		Path:        target,
		Filesystems: filesystems,
		CheckedAt:   a.now(),
		Raw:         strings.TrimSpace(string(output)),
	}, nil
}

func (a *Adapter) ProbeTopology(ctx context.Context, req domain.TopologyRequest) (domain.TopologyProbe, error) {
	if strings.TrimSpace(req.SlurmJobID) != "" {
		return a.probeAllocationTopology(ctx, req)
	}
	return a.probeEphemeralTopology(ctx, req)
}

func (a *Adapter) QueryStatus(ctx context.Context, slurmJobID string) (JobStatus, error) {
	if slurmJobID == "" {
		return JobStatus{}, fmt.Errorf("slurm job id is required")
	}
	output, err := a.run(ctx, "squeue", "--noheader", "--format=%i|%T|%N|%R", "--job", slurmJobID)
	if err == nil {
		if status, ok := parseSQueue(strings.TrimSpace(string(output))); ok {
			return status, nil
		}
	}
	output, err = a.run(ctx, "sacct", "-j", slurmJobID, "--format=JobIDRaw,State,ExitCode,NodeList,Start,End", "-P", "-n")
	if err != nil {
		return JobStatus{}, fmt.Errorf("sacct failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	status, ok := parseSACCT(slurmJobID, strings.TrimSpace(string(output)))
	if !ok {
		return JobStatus{}, fmt.Errorf("unable to parse slurm status for job %s", slurmJobID)
	}
	return status, nil
}

func (a *Adapter) ExpandNodeList(ctx context.Context, raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	output, err := a.run(ctx, "scontrol", "show", "hostnames", raw)
	if err != nil {
		return nil, fmt.Errorf("expand node list failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	nodes := make([]string, 0)
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		nodes = append(nodes, line)
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("node list expansion returned no hosts")
	}
	return nodes, nil
}

func (a *Adapter) probeAllocationTopology(ctx context.Context, req domain.TopologyRequest) (domain.TopologyProbe, error) {
	nodes := make([]string, 0, 1)
	if strings.TrimSpace(req.Node) != "" {
		nodes = append(nodes, strings.TrimSpace(req.Node))
	} else {
		status, err := a.QueryStatus(ctx, req.SlurmJobID)
		if err != nil {
			return domain.TopologyProbe{}, err
		}
		for _, rawNodeList := range status.NodeList {
			expanded, err := a.ExpandNodeList(ctx, rawNodeList)
			if err != nil {
				return domain.TopologyProbe{}, err
			}
			nodes = append(nodes, expanded...)
		}
	}
	nodes = dedupeStrings(nodes)
	if len(nodes) == 0 {
		return domain.TopologyProbe{}, fmt.Errorf("no allocated nodes available for Slurm job %s", req.SlurmJobID)
	}
	probe := domain.TopologyProbe{
		Mode:       string(TopologyProbeAllocation),
		JobID:      req.JobID,
		SlurmJobID: req.SlurmJobID,
		CheckedAt:  a.now(),
		Nodes:      make([]domain.TopologyNode, 0, len(nodes)),
	}
	for _, node := range nodes {
		command := fmt.Sprintf(
			"srun --jobid %s --overlap --nodes=1 --ntasks=1 --nodelist %s bash -lc %s",
			shellQuote(req.SlurmJobID),
			shellQuote(node),
			shellQuote(topologyProbeScript()),
		)
		output, err := a.runShell(ctx, command)
		if err != nil {
			return domain.TopologyProbe{}, fmt.Errorf("allocation topology probe failed for %s: %w: %s", node, err, strings.TrimSpace(string(output)))
		}
		entry, err := parseTopologyOutput(node, string(output))
		if err != nil {
			return domain.TopologyProbe{}, err
		}
		probe.Nodes = append(probe.Nodes, entry)
	}
	return probe, nil
}

func (a *Adapter) probeEphemeralTopology(ctx context.Context, req domain.TopologyRequest) (domain.TopologyProbe, error) {
	gpus := req.GPUs
	if gpus <= 0 {
		gpus = 8
	}
	cpus := req.CPUs
	if cpus <= 0 {
		cpus = 16
	}
	memoryMB := req.MemoryMB
	if memoryMB <= 0 {
		memoryMB = 100 * 1024
	}
	walltime := strings.TrimSpace(req.Walltime)
	if walltime == "" {
		walltime = "00:05:00"
	}
	immediate := req.ImmediateSeconds
	if immediate <= 0 {
		immediate = 60
	}
	command := fmt.Sprintf(
		"srun --immediate=%d --gres=gpu:%d --cpus-per-task=%d --mem=%dM --time=%s --ntasks=1 bash -lc %s",
		immediate,
		gpus,
		cpus,
		memoryMB,
		shellQuote(walltime),
		shellQuote(topologyProbeScript()),
	)
	output, err := a.runShell(ctx, command)
	if err != nil {
		return domain.TopologyProbe{}, fmt.Errorf("ephemeral topology probe failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	entry, err := parseTopologyOutput("", string(output))
	if err != nil {
		return domain.TopologyProbe{}, err
	}
	return domain.TopologyProbe{
		Mode:          string(TopologyProbeEphemeral),
		RequestedGPUs: gpus,
		CheckedAt:     a.now(),
		Nodes:         []domain.TopologyNode{entry},
	}, nil
}

func topologyProbeScript() string {
	return "hostname; echo '---'; nvidia-smi topo -m"
}

func parseSQueue(raw string) (JobStatus, bool) {
	if raw == "" {
		return JobStatus{}, false
	}
	parts := strings.Split(raw, "|")
	if len(parts) < 4 {
		return JobStatus{}, false
	}
	nodeList := []string{}
	if trimmed := strings.TrimSpace(parts[2]); trimmed != "" && trimmed != "n/a" && trimmed != "N/A" && trimmed != "(null)" {
		nodeList = []string{trimmed}
	}
	return JobStatus{
		SlurmJobID: strings.TrimSpace(parts[0]),
		State:      strings.TrimSpace(parts[1]),
		NodeList:   nodeList,
	}, true
}

func parseSACCT(slurmJobID, raw string) (JobStatus, bool) {
	if raw == "" {
		return JobStatus{}, false
	}
	for _, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 6 {
			continue
		}
		if strings.TrimSpace(parts[0]) != slurmJobID {
			continue
		}
		status := JobStatus{
			SlurmJobID: slurmJobID,
			State:      strings.TrimSpace(parts[1]),
		}
		if exitParts := strings.Split(strings.TrimSpace(parts[2]), ":"); len(exitParts) > 0 {
			if code, err := strconv.Atoi(exitParts[0]); err == nil {
				status.ExitCode = &code
			}
		}
		if nodeList := strings.TrimSpace(parts[3]); nodeList != "" {
			status.NodeList = []string{nodeList}
		}
		status.StartedAt = parseTime(parts[4])
		status.FinishedAt = parseTime(parts[5])
		return status, true
	}
	return JobStatus{}, false
}

func parseTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "Unknown" || raw == "N/A" {
		return time.Time{}
	}
	layouts := []string{
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05-0700",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func parseDFOutput(raw string) ([]domain.StorageFilesystem, error) {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("unexpected df output")
	}
	filesystems := make([]domain.StorageFilesystem, 0, len(lines)-1)
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 6 {
			return nil, fmt.Errorf("unexpected df row %q", line)
		}
		sizeBytes, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse df size: %w", err)
		}
		usedBytes, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse df used: %w", err)
		}
		availableBytes, err := strconv.ParseInt(fields[3], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse df avail: %w", err)
		}
		usePercent, err := strconv.Atoi(strings.TrimSuffix(fields[4], "%"))
		if err != nil {
			return nil, fmt.Errorf("parse df use percent: %w", err)
		}
		filesystems = append(filesystems, domain.StorageFilesystem{
			Source:         fields[0],
			Target:         fields[5],
			SizeBytes:      sizeBytes,
			UsedBytes:      usedBytes,
			AvailableBytes: availableBytes,
			UsePercent:     usePercent,
		})
	}
	if len(filesystems) == 0 {
		return nil, fmt.Errorf("df returned no filesystems")
	}
	return filesystems, nil
}

func parseTopologyOutput(fallbackNode, raw string) (domain.TopologyNode, error) {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(raw), "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		return domain.TopologyNode{}, fmt.Errorf("topology probe returned no output")
	}
	separator := -1
	for idx, line := range lines {
		if strings.TrimSpace(line) == "---" {
			separator = idx
			break
		}
	}
	node := strings.TrimSpace(fallbackNode)
	if separator >= 0 {
		for idx := separator - 1; idx >= 0; idx-- {
			if candidate := strings.TrimSpace(lines[idx]); candidate != "" {
				node = candidate
				break
			}
		}
		matrix := strings.TrimSpace(strings.Join(lines[separator+1:], "\n"))
		if node == "" {
			return domain.TopologyNode{}, fmt.Errorf("topology probe did not report a node")
		}
		if matrix == "" {
			return domain.TopologyNode{}, fmt.Errorf("topology probe did not return a matrix")
		}
		return domain.TopologyNode{Node: node, Matrix: matrix}, nil
	}
	if len(lines) == 1 {
		return domain.TopologyNode{}, fmt.Errorf("topology probe did not return a matrix")
	}
	if node == "" {
		node = strings.TrimSpace(lines[0])
	}
	matrix := strings.TrimSpace(strings.Join(lines[1:], "\n"))
	if node == "" || matrix == "" {
		return domain.TopologyNode{}, fmt.Errorf("topology probe did not return a usable host and matrix")
	}
	return domain.TopologyNode{Node: node, Matrix: matrix}, nil
}

func sortStrings(items []string) {
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j] < items[i] {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}

func (a *Adapter) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if a.remoteHost == "" {
		return a.runner.Run(ctx, name, args...)
	}
	sshArgs := append([]string{a.remoteHost, name}, args...)
	return a.runner.Run(ctx, "ssh", sshArgs...)
}

func (a *Adapter) runShell(ctx context.Context, command string) ([]byte, error) {
	if a.remoteHost == "" {
		return a.runner.Run(ctx, "bash", "-lc", command)
	}
	return a.runner.Run(ctx, "ssh", a.remoteHost, fmt.Sprintf("bash -lc %s", shellQuote(command)))
}

func (a *Adapter) runSubmit(ctx context.Context, scriptPath string) ([]byte, error) {
	if a.remoteHost == "" {
		return a.runner.Run(ctx, "sbatch", "--parsable", scriptPath)
	}
	scriptBytes, err := os.ReadFile(scriptPath)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "ssh", a.remoteHost, "sbatch", "--parsable")
	cmd.Stdin = bytes.NewReader(scriptBytes)
	return cmd.CombinedOutput()
}

func readLocalLog(logPath string, tailLines int) ([]byte, error) {
	if tailLines <= 0 {
		return os.ReadFile(logPath)
	}
	file, err := os.Open(logPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	lines := make([]string, 0, tailLines)
	for {
		line, readErr := reader.ReadString('\n')
		if len(line) > 0 {
			if len(lines) < tailLines {
				lines = append(lines, line)
			} else {
				copy(lines, lines[1:])
				lines[len(lines)-1] = line
			}
		}
		if readErr == nil {
			continue
		}
		if readErr == io.EOF {
			break
		}
		return nil, readErr
	}
	return []byte(strings.Join(lines, "")), nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func dedupeStrings(items []string) []string {
	if len(items) < 2 {
		return items
	}
	seen := make(map[string]struct{}, len(items))
	deduped := make([]string, 0, len(items))
	for _, item := range items {
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		deduped = append(deduped, item)
	}
	return deduped
}
