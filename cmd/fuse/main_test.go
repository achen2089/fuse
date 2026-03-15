package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fuse/internal/domain"
	"fuse/internal/recipes"
)

func TestLoadJobSpecFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "job.json")
	if err := os.WriteFile(path, []byte(`{
  "name": "spec-smoke",
  "team": "default",
  "type": "run",
  "command_or_recipe": "/bin/true",
  "gpus": 1,
  "cpus": 4,
  "memory_mb": 8192,
  "walltime": "00:05:00"
}`), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	spec, err := loadJobSpec(path)
	if err != nil {
		t.Fatalf("load job spec: %v", err)
	}
	if spec.Name != "spec-smoke" || spec.CommandOrRecipe != "/bin/true" || spec.GPUs != 1 {
		t.Fatalf("unexpected spec: %#v", spec)
	}
}

func TestSubmitSpecPostsJobAndPrintsSlurmID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "job.json")
	if err := os.WriteFile(path, []byte(`{
  "name": "submit-smoke",
  "team": "default",
  "type": "run",
  "command_or_recipe": "/bin/true",
  "gpus": 1,
  "cpus": 4,
  "memory_mb": 8192,
  "walltime": "00:05:00"
}`), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	cli := &fakeCLI{
		submitJob: domain.Job{
			ID:         "submit-smoke",
			State:      domain.JobStatePending,
			SlurmJobID: "12345",
		},
	}

	output := captureStdout(t, func() {
		if err := submitSpec(context.Background(), cli, []string{path}, false); err != nil {
			t.Fatalf("submit spec: %v", err)
		}
	})

	if cli.submittedSpec.Name != "submit-smoke" || cli.submittedSpec.CommandOrRecipe != "/bin/true" {
		t.Fatalf("unexpected posted spec: %#v", cli.submittedSpec)
	}
	if !strings.Contains(output, "slurm=12345") {
		t.Fatalf("expected slurm id in output, got %q", output)
	}
}

func TestSubmitRunQuotesCommandAndForwardsContainerOptions(t *testing.T) {
	cli := &fakeCLI{
		submitJob: domain.Job{
			ID:         "container-smoke",
			State:      domain.JobStatePending,
			SlurmJobID: "22222",
		},
	}

	output := captureStdout(t, func() {
		err := submitRun(context.Background(), cli, []string{
			"--name", "container-smoke",
			"--gpus", "1",
			"--image", "/mnt/sharefs/user44/fuse-ngc-pytorch-2502.sqsh",
			"--mount-home",
			"--mount", "/mnt/sharefs/user44:/mnt/sharefs/user44",
			"--container-workdir", "/mnt/sharefs/user44",
			"--env", "NCCL_IB_DISABLE=1",
			"--env", "NCCL_SOCKET_IFNAME=enp71s0",
			"--",
			"bash", "-lc", `python -c "import torch; print(torch.__version__)"`,
		}, false)
		if err != nil {
			t.Fatalf("submit run: %v", err)
		}
	})

	if !strings.Contains(output, "slurm=22222") {
		t.Fatalf("expected slurm id in output, got %q", output)
	}
	if got, want := cli.submittedSpec.CommandOrRecipe, `'bash' '-lc' 'python -c "import torch; print(torch.__version__)"'`; got != want {
		t.Fatalf("unexpected quoted command:\n got: %s\nwant: %s", got, want)
	}
	if cli.submittedSpec.ContainerImage != "/mnt/sharefs/user44/fuse-ngc-pytorch-2502.sqsh" {
		t.Fatalf("unexpected container image: %#v", cli.submittedSpec)
	}
	if !cli.submittedSpec.ContainerMountHome || cli.submittedSpec.ContainerWorkdir != "/mnt/sharefs/user44" {
		t.Fatalf("unexpected container settings: %#v", cli.submittedSpec)
	}
	if len(cli.submittedSpec.ContainerMounts) != 1 || cli.submittedSpec.ContainerMounts[0] != "/mnt/sharefs/user44:/mnt/sharefs/user44" {
		t.Fatalf("unexpected container mounts: %#v", cli.submittedSpec.ContainerMounts)
	}
	if cli.submittedSpec.Env["NCCL_IB_DISABLE"] != "1" || cli.submittedSpec.Env["NCCL_SOCKET_IFNAME"] != "enp71s0" {
		t.Fatalf("unexpected env: %#v", cli.submittedSpec.Env)
	}
}

func TestSubmitTrainBuildsMakemoreSpec(t *testing.T) {
	cli := &fakeCLI{
		submitJob: domain.Job{
			ID:         "makemore-train",
			State:      domain.JobStatePending,
			SlurmJobID: "33333",
		},
	}

	output := captureStdout(t, func() {
		err := submitTrain(context.Background(), cli, []string{
			"--example", "makemore",
			"--name", "makemore-train",
			"--steps", "123",
		}, false)
		if err != nil {
			t.Fatalf("submit train: %v", err)
		}
	})

	if !strings.Contains(output, "slurm=33333") {
		t.Fatalf("expected slurm id in output, got %q", output)
	}
	if cli.submittedSpec.Type != domain.JobTypeTrain || cli.submittedSpec.ContainerImage != recipes.DefaultMakemoreImage {
		t.Fatalf("unexpected train spec: %#v", cli.submittedSpec)
	}
	if !strings.Contains(cli.submittedSpec.CommandOrRecipe, "makemore_smoke.py") || !strings.Contains(cli.submittedSpec.CommandOrRecipe, "--steps 123") {
		t.Fatalf("unexpected train command: %s", cli.submittedSpec.CommandOrRecipe)
	}
}

func TestSubmitTrainBuildsNanochatTorchrunSpec(t *testing.T) {
	cli := &fakeCLI{
		submitJob: domain.Job{
			ID:         "nanochat-train",
			State:      domain.JobStatePending,
			SlurmJobID: "44444",
		},
	}

	if err := submitTrain(context.Background(), cli, []string{
		"--example", "nanochat",
		"--name", "nanochat-train",
		"--gpus", "4",
	}, false); err != nil {
		t.Fatalf("submit train: %v", err)
	}

	if !strings.HasPrefix(cli.submittedSpec.CommandOrRecipe, "torchrun --standalone") {
		t.Fatalf("unexpected train command: %s", cli.submittedSpec.CommandOrRecipe)
	}
	if cli.submittedSpec.Env["OMP_NUM_THREADS"] != "1" {
		t.Fatalf("expected OMP_NUM_THREADS env, got %#v", cli.submittedSpec.Env)
	}
}

func TestSubmitTrainBuildsMultiNodeNanochatSpec(t *testing.T) {
	cli := &fakeCLI{
		submitJob: domain.Job{
			ID:         "nanochat-multinode",
			State:      domain.JobStatePending,
			SlurmJobID: "44445",
		},
	}

	if err := submitTrain(context.Background(), cli, []string{
		"--example", "nanochat",
		"--name", "nanochat-multinode",
		"--gpus", "16",
	}, false); err != nil {
		t.Fatalf("submit train: %v", err)
	}

	if cli.submittedSpec.Nodes != 2 || cli.submittedSpec.Tasks != 2 || cli.submittedSpec.TasksPerNode != 1 || cli.submittedSpec.GPUsPerNode != 8 {
		t.Fatalf("unexpected multinode spec: %#v", cli.submittedSpec)
	}
	if got := cli.submittedSpec.CommandOrRecipe; !strings.Contains(got, "srun --ntasks=2 --ntasks-per-node=1") || !strings.Contains(got, "torchrun --nnodes=2") {
		t.Fatalf("unexpected multinode train command: %s", got)
	}
	if cli.submittedSpec.Env["NCCL_IB_DISABLE"] != "1" || cli.submittedSpec.Env["NCCL_SOCKET_IFNAME"] != "enp71s0" {
		t.Fatalf("unexpected multinode env: %#v", cli.submittedSpec.Env)
	}
}

func TestSubmitTrainSupportsHoldAndAxolotlProbe(t *testing.T) {
	cli := &fakeCLI{
		submitJob: domain.Job{
			ID:         "axolotl-train",
			State:      domain.JobStatePending,
			SlurmJobID: "55555",
		},
	}

	if err := submitTrain(context.Background(), cli, []string{
		"--example", "axolotl-probe",
		"--name", "axolotl-train",
		"--hold", "90",
	}, false); err != nil {
		t.Fatalf("submit train: %v", err)
	}

	if cli.submittedSpec.Type != domain.JobTypeTrain {
		t.Fatalf("unexpected train type: %#v", cli.submittedSpec)
	}
	if got := cli.submittedSpec.CommandOrRecipe; !strings.Contains(got, "axolotl_probe.py") || !strings.Contains(got, "sleep 90") {
		t.Fatalf("unexpected hold command: %s", got)
	}
}

func TestShowShardPrintsPlan(t *testing.T) {
	cli := &fakeCLI{
		shardOutput: domain.ShardPlan{
			Model:                   "llama-70b",
			Method:                  "full",
			GPUs:                    16,
			Nodes:                   2,
			GPUsPerNode:             8,
			TensorParallel:          8,
			PipelineParallel:        2,
			DataParallel:            1,
			PerGPUWeightGB:          8.8,
			EstimatedPerGPUMemoryGB: 35.4,
			DeviceMemoryGB:          179.1,
			TopologyHint:            domain.TopologySameSwitch,
			Summary:                 "llama-70b recommends TP=8 PP=2 DP=1 on 16 GPUs",
			Detail:                  "TP=8, PP=2, DP=1 keeps 35.4 GB/GPU under 179.1 GB device memory",
			Suggestions:             []string{"prefer same_switch placement"},
		},
	}

	output := captureStdout(t, func() {
		if err := showShard(context.Background(), cli, []string{"--model", "llama-70b", "--gpus", "16"}, false); err != nil {
			t.Fatalf("show shard: %v", err)
		}
	})

	if cli.shardRequest.Model != "llama-70b" || cli.shardRequest.GPUs != 16 {
		t.Fatalf("unexpected shard request: %#v", cli.shardRequest)
	}
	if !strings.Contains(output, "tp=8") || !strings.Contains(output, "topology=same_switch") {
		t.Fatalf("unexpected shard output: %q", output)
	}
}

func TestShowLogsPrintsContent(t *testing.T) {
	cli := &fakeCLI{
		logOutput: domain.JobLog{
			JobID:   "log-smoke",
			Stream:  "stdout",
			Content: "hello from logs\n",
		},
	}

	output := captureStdout(t, func() {
		if err := showLogs(context.Background(), cli, []string{"log-smoke"}, false); err != nil {
			t.Fatalf("show logs: %v", err)
		}
	})

	if output != "hello from logs\n" {
		t.Fatalf("unexpected log output: %q", output)
	}
	if cli.logsJobID != "log-smoke" || cli.logsStream != "stdout" || cli.logsTail != 200 {
		t.Fatalf("unexpected logs request: job=%q stream=%q tail=%d", cli.logsJobID, cli.logsStream, cli.logsTail)
	}
}

func TestShowStoragePrintsFilesystemSummary(t *testing.T) {
	cli := &fakeCLI{
		storageOutput: domain.StorageStatus{
			Path: "/mnt/sharefs/user44",
			Filesystems: []domain.StorageFilesystem{{
				Source:         "10.0.0.10@o2ib:/lustre",
				Target:         "/mnt/sharefs/user44",
				SizeBytes:      61 * 1024 * 1024 * 1024 * 1024,
				UsedBytes:      21 * 1024 * 1024 * 1024 * 1024,
				AvailableBytes: 40 * 1024 * 1024 * 1024 * 1024,
				UsePercent:     35,
			}},
		},
	}

	output := captureStdout(t, func() {
		if err := showStorage(context.Background(), cli, nil, false); err != nil {
			t.Fatalf("show storage: %v", err)
		}
	})

	if cli.storagePath != "" {
		t.Fatalf("expected default storage path lookup, got %q", cli.storagePath)
	}
	if !strings.Contains(output, "/mnt/sharefs/user44") || !strings.Contains(output, "use=35%") {
		t.Fatalf("unexpected storage output: %q", output)
	}
}

func TestShowTopologyPrintsProbeDetails(t *testing.T) {
	cli := &fakeCLI{
		topologyOutput: domain.TopologyProbe{
			Mode:          "allocation",
			JobID:         "probe-smoke",
			SlurmJobID:    "12345",
			RequestedGPUs: 8,
			Nodes: []domain.TopologyNode{{
				Node:   "us-west-a2-gpu-014",
				Matrix: "GPU0\tGPU1\nGPU0\tX\tNV18",
			}},
		},
	}

	output := captureStdout(t, func() {
		if err := showTopology(context.Background(), cli, []string{"--job", "probe-smoke"}, false); err != nil {
			t.Fatalf("show topology: %v", err)
		}
	})

	if cli.topologyRequest.JobID != "probe-smoke" {
		t.Fatalf("unexpected topology request: %#v", cli.topologyRequest)
	}
	if !strings.Contains(output, "mode=allocation") || !strings.Contains(output, "[us-west-a2-gpu-014]") {
		t.Fatalf("unexpected topology output: %q", output)
	}
}

func TestShowStoragePrintsJSON(t *testing.T) {
	cli := &fakeCLI{
		storageOutput: domain.StorageStatus{
			Path: "/mnt/sharefs/user44",
			Filesystems: []domain.StorageFilesystem{{
				Source:         "10.0.0.10@o2ib:/lustre",
				Target:         "/mnt/sharefs",
				SizeBytes:      1024,
				UsedBytes:      512,
				AvailableBytes: 512,
				UsePercent:     50,
			}},
		},
	}

	output := captureStdout(t, func() {
		if err := showStorage(context.Background(), cli, nil, true); err != nil {
			t.Fatalf("show storage json: %v", err)
		}
	})

	if !strings.Contains(output, "\"path\": \"/mnt/sharefs/user44\"") || !strings.Contains(output, "\"use_percent\": 50") {
		t.Fatalf("unexpected storage json output: %q", output)
	}
}

func TestRunDoctorClusterPrintsChecks(t *testing.T) {
	cli := &fakeCLI{
		statusOutput: domain.ClusterStatus{
			Nodes:       32,
			Devices:     256,
			Allocated:   80,
			Idle:        176,
			RunningJobs: 4,
			PendingJobs: 1,
			FailedJobs:  0,
		},
		nodesOutput: []domain.Node{
			{Name: "gpu-001", Health: domain.HealthHealthy, ObservedState: "idle"},
			{Name: "gpu-002", Health: domain.HealthHealthy, ObservedState: "mix"},
		},
		storageOutput: domain.StorageStatus{
			Path: "/mnt/sharefs",
			Filesystems: []domain.StorageFilesystem{{
				Source:         "10.0.0.10@o2ib:/lustre",
				Target:         "/mnt/sharefs",
				SizeBytes:      61 * 1024 * 1024 * 1024 * 1024,
				UsedBytes:      2 * 1024 * 1024 * 1024 * 1024,
				AvailableBytes: 59 * 1024 * 1024 * 1024 * 1024,
				UsePercent:     3,
			}},
		},
	}

	output := captureStdout(t, func() {
		if err := runDoctor(context.Background(), cli, []string{"cluster"}, false); err != nil {
			t.Fatalf("run doctor cluster: %v", err)
		}
	})

	if cli.storagePath != "/mnt/sharefs" {
		t.Fatalf("unexpected storage path: %q", cli.storagePath)
	}
	if !strings.Contains(output, "scope=cluster") || !strings.Contains(output, "check[inventory]=ok") || !strings.Contains(output, "check[storage]=ok") {
		t.Fatalf("unexpected doctor output: %q", output)
	}
}

func TestRunDoctorJobPrintsChecks(t *testing.T) {
	cli := &fakeCLI{
		jobsOutput: []domain.Job{{
			ID:             "run-123",
			Name:           "makemore-demo",
			State:          domain.JobStatePending,
			CheckpointMode: domain.CheckpointFilesystem,
		}},
		whyOutput: domain.Why{
			JobID:        "run-123",
			ReasonCode:   domain.ReasonSlurmQueueBacklog,
			Summary:      "job is pending in Slurm",
			Detail:       "Slurm has accepted the job but it is not running yet",
			Suggestions:  []string{"wait for capacity", "use fuse why"},
			CurrentState: domain.JobStatePending,
			RawState:     "PENDING",
		},
		checkpointsOut: []domain.Checkpoint{{
			JobID:     "run-123",
			Path:      "/mnt/sharefs/user44/ckpt/step-100",
			StepLabel: "step-100",
			Verified:  true,
		}},
	}

	output := captureStdout(t, func() {
		if err := runDoctor(context.Background(), cli, []string{"run-123"}, false); err != nil {
			t.Fatalf("run doctor job: %v", err)
		}
	})

	if cli.whyJobID != "run-123" {
		t.Fatalf("unexpected why job id: %q", cli.whyJobID)
	}
	if !strings.Contains(output, "scope=job") || !strings.Contains(output, "check[state]=warn") || !strings.Contains(output, "check[checkpoints]=ok") {
		t.Fatalf("unexpected doctor job output: %q", output)
	}
}

func TestResolveDirectSSHHostUsesLiveDefaultByDefault(t *testing.T) {
	got := resolveDirectSSHHost(defaultDirectSSHHost, false, "", false, false)
	if got != defaultDirectSSHHost {
		t.Fatalf("unexpected ssh host: %q", got)
	}
}

func TestResolveDirectSSHHostClearsDefaultForFaker(t *testing.T) {
	got := resolveDirectSSHHost(defaultDirectSSHHost, false, "", true, false)
	if got != "" {
		t.Fatalf("expected faker mode to clear default ssh host, got %q", got)
	}
}

func TestShouldLaunchDefaultTUI(t *testing.T) {
	if !shouldLaunchDefaultTUI(nil) {
		t.Fatalf("expected bare fuse to launch default TUI")
	}
	if !shouldLaunchDefaultTUI([]string{"--faker"}) {
		t.Fatalf("expected root TUI flags to launch default TUI")
	}
	if shouldLaunchDefaultTUI([]string{"--help"}) {
		t.Fatalf("did not expect --help to launch default TUI")
	}
	if shouldLaunchDefaultTUI([]string{"status"}) {
		t.Fatalf("did not expect explicit subcommands to launch default TUI")
	}
}

func TestWantsHelp(t *testing.T) {
	if !wantsHelp([]string{"--help"}) {
		t.Fatalf("expected --help to request usage")
	}
	if !wantsHelp([]string{"help"}) {
		t.Fatalf("expected help command to request usage")
	}
	if !wantsHelp([]string{"tui", "--help"}) {
		t.Fatalf("expected fuse tui --help to request usage")
	}
	if !wantsHelp([]string{"run", "--faker", "--help"}) {
		t.Fatalf("expected help flags after command flags to request usage")
	}
	if wantsHelp([]string{"run", "--", "--help"}) {
		t.Fatalf("did not expect help after -- to request usage")
	}
	if wantsHelp([]string{"status"}) {
		t.Fatalf("did not expect status to request usage")
	}
}

func TestUsageMentionsCanonicalAndCompatBinaryPaths(t *testing.T) {
	output := captureStdout(t, func() {
		usage()
	})
	if !strings.Contains(output, "make build") {
		t.Fatalf("expected usage to mention make build, got %q", output)
	}
	if !strings.Contains(output, "./fuse") || !strings.Contains(output, "canonical repo-local binary") {
		t.Fatalf("expected usage to mention canonical ./fuse path, got %q", output)
	}
	if !strings.Contains(output, "./.bin/fuse-live") {
		t.Fatalf("expected usage to mention compat alias, got %q", output)
	}
	if !strings.Contains(output, "Avoid: fuse --faker status") {
		t.Fatalf("expected usage to explain command ordering, got %q", output)
	}
	if !strings.Contains(output, "fuse help run") {
		t.Fatalf("expected usage to advertise command-specific help, got %q", output)
	}
}

func TestHelpTopicFromArgs(t *testing.T) {
	if got := helpTopicFromArgs([]string{"help", "run"}); got != "run" {
		t.Fatalf("expected help topic run, got %q", got)
	}
	if got := helpTopicFromArgs([]string{"run", "--faker", "--help"}); got != "run" {
		t.Fatalf("expected run --faker --help to resolve to run, got %q", got)
	}
	if got := helpTopicFromArgs([]string{"--faker", "--help"}); got != "" {
		t.Fatalf("expected root help for leading flags, got %q", got)
	}
}

func TestPrintHelpForRunTopic(t *testing.T) {
	output := captureStdout(t, func() {
		printHelp([]string{"help", "run"})
	})
	if !strings.Contains(output, "Fuse run") {
		t.Fatalf("expected run topic header, got %q", output)
	}
	if !strings.Contains(output, "Everything after `--` becomes the remote command line.") {
		t.Fatalf("expected run topic summary, got %q", output)
	}
	if !strings.Contains(output, "--mount SRC:DST[:FLAGS]") {
		t.Fatalf("expected run topic flags, got %q", output)
	}
}

func TestPrintHelpUnknownTopicFallsBackToUsage(t *testing.T) {
	output := captureStdout(t, func() {
		printHelp([]string{"help", "does-not-exist"})
	})
	if !strings.Contains(output, `Unknown help topic "does-not-exist".`) {
		t.Fatalf("expected unknown-topic message, got %q", output)
	}
	if !strings.Contains(output, "Command ordering") {
		t.Fatalf("expected root usage after unknown-topic message, got %q", output)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	defer reader.Close()
	original := os.Stdout
	os.Stdout = writer
	defer func() {
		os.Stdout = original
	}()

	fn()

	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return string(data)
}

type fakeCLI struct {
	submitJob       domain.Job
	submitErr       error
	submittedSpec   domain.JobSpec
	statusOutput    domain.ClusterStatus
	statusErr       error
	nodesOutput     []domain.Node
	devicesOutput   []domain.Device
	nodesErr        error
	jobsOutput      []domain.Job
	jobsErr         error
	shardOutput     domain.ShardPlan
	shardErr        error
	shardRequest    domain.ShardRequest
	logOutput       domain.JobLog
	logErr          error
	logsJobID       string
	logsStream      string
	logsTail        int
	storageOutput   domain.StorageStatus
	storageErr      error
	storagePath     string
	topologyOutput  domain.TopologyProbe
	topologyErr     error
	topologyRequest domain.TopologyRequest
	whyOutput       domain.Why
	whyErr          error
	whyJobID        string
	checkpointsOut  []domain.Checkpoint
	checkpointsErr  error
}

func (f *fakeCLI) Status(context.Context) (domain.ClusterStatus, error) {
	return f.statusOutput, f.statusErr
}

func (f *fakeCLI) Nodes(context.Context) ([]domain.Node, []domain.Device, error) {
	return f.nodesOutput, f.devicesOutput, f.nodesErr
}

func (f *fakeCLI) Fabric(context.Context) ([]domain.FabricLink, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeCLI) Teams(context.Context) ([]domain.Team, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeCLI) Jobs(context.Context) ([]domain.Job, error) {
	return f.jobsOutput, f.jobsErr
}

func (f *fakeCLI) Events(context.Context, int) ([]domain.Event, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeCLI) Storage(_ context.Context, target string) (domain.StorageStatus, error) {
	f.storagePath = target
	return f.storageOutput, f.storageErr
}

func (f *fakeCLI) Topology(_ context.Context, req domain.TopologyRequest) (domain.TopologyProbe, error) {
	f.topologyRequest = req
	return f.topologyOutput, f.topologyErr
}

func (f *fakeCLI) Shard(_ context.Context, req domain.ShardRequest) (domain.ShardPlan, error) {
	f.shardRequest = req
	return f.shardOutput, f.shardErr
}

func (f *fakeCLI) Submit(_ context.Context, spec domain.JobSpec) (domain.Job, error) {
	f.submittedSpec = spec
	return f.submitJob, f.submitErr
}

func (f *fakeCLI) Logs(_ context.Context, jobID, stream string, tailLines int) (domain.JobLog, error) {
	f.logsJobID = jobID
	f.logsStream = stream
	f.logsTail = tailLines
	return f.logOutput, f.logErr
}

func (f *fakeCLI) Why(_ context.Context, jobID string) (domain.Why, error) {
	f.whyJobID = jobID
	return f.whyOutput, f.whyErr
}

func (f *fakeCLI) Cancel(context.Context, string) error {
	return errors.New("not implemented")
}

func (f *fakeCLI) Checkpoint(context.Context, string) error {
	return errors.New("not implemented")
}

func (f *fakeCLI) Checkpoints(context.Context, string) ([]domain.Checkpoint, error) {
	return f.checkpointsOut, f.checkpointsErr
}

func (f *fakeCLI) Simulate(context.Context, domain.SimulationRequest) (domain.SimulationResult, error) {
	return domain.SimulationResult{}, errors.New("not implemented")
}
