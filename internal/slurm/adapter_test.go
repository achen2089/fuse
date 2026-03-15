package slurm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fuse/internal/domain"
)

type fakeRunner struct {
	outputs map[string][]byte
	err     error
	calls   []string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	key := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, key)
	if out, ok := f.outputs[key]; ok {
		return out, f.err
	}
	return nil, f.err
}

func TestRenderScriptIncludesMetadata(t *testing.T) {
	adapter := New(&fakeRunner{}, "")
	script := adapter.RenderScript(domain.JobSpec{
		ID:              "job-1",
		Name:            "job-1",
		Type:            domain.JobTypeRun,
		CommandOrRecipe: "python train.py",
		GPUs:            2,
		CPUs:            8,
		MemoryMB:        32768,
		Walltime:        "00:30:00",
		CheckpointDir:   "/tmp/ckpt",
		ArtifactsDir:    "/tmp/artifacts",
		Env:             map[string]string{"FOO": "bar"},
	})
	if !strings.Contains(script, "#SBATCH --gres=gpu:2") {
		t.Fatalf("expected GPU request in script:\n%s", script)
	}
	if !strings.Contains(script, `export FUSE_JOB_ID="job-1"`) {
		t.Fatalf("expected job metadata in script:\n%s", script)
	}
	if !strings.Contains(script, "python train.py") {
		t.Fatalf("expected command in script:\n%s", script)
	}
}

func TestRenderScriptIncludesContainerDirectives(t *testing.T) {
	adapter := New(&fakeRunner{}, "")
	script := adapter.RenderScript(domain.JobSpec{
		ID:                 "job-2",
		Name:               "job-2",
		Type:               domain.JobTypeRun,
		CommandOrRecipe:    "python -c 'import torch'",
		Workdir:            "/mnt/sharefs/user44",
		ContainerImage:     "/mnt/sharefs/user44/fuse-ngc-pytorch-2502.sqsh",
		ContainerMounts:    []string{"/mnt/sharefs/user44:/mnt/sharefs/user44"},
		ContainerWorkdir:   "/mnt/sharefs/user44",
		ContainerMountHome: true,
		GPUs:               1,
		CPUs:               4,
		MemoryMB:           32768,
		Walltime:           "00:05:00",
		ArtifactsDir:       "/mnt/sharefs/user44/.fuse/artifacts/job-2",
	})
	for _, fragment := range []string{
		"#SBATCH --container-image=/mnt/sharefs/user44/fuse-ngc-pytorch-2502.sqsh",
		"#SBATCH --container-mounts=/mnt/sharefs/user44:/mnt/sharefs/user44",
		"#SBATCH --container-workdir=/mnt/sharefs/user44",
		"#SBATCH --container-mount-home",
		`cd "/mnt/sharefs/user44"`,
	} {
		if !strings.Contains(script, fragment) {
			t.Fatalf("expected %q in script:\n%s", fragment, script)
		}
	}
}

func TestParseSACCT(t *testing.T) {
	status, ok := parseSACCT("123", "123|COMPLETED|0:0|n1|2025-03-15T12:00:00|2025-03-15T12:10:00")
	if !ok {
		t.Fatal("expected parse success")
	}
	if status.State != "COMPLETED" {
		t.Fatalf("expected COMPLETED, got %s", status.State)
	}
	if status.ExitCode == nil || *status.ExitCode != 0 {
		t.Fatalf("unexpected exit code: %#v", status.ExitCode)
	}
}

func TestParseSQueue(t *testing.T) {
	testCases := []struct {
		name         string
		raw          string
		wantNodeList []string
	}{
		{
			name:         "running job returns nodelist",
			raw:          "123|RUNNING|us-west-a2-gpu-015|(null)",
			wantNodeList: []string{"us-west-a2-gpu-015"},
		},
		{
			name:         "pending job keeps node list empty",
			raw:          "123|PENDING|n/a|(Priority)",
			wantNodeList: nil,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			status, ok := parseSQueue(tc.raw)
			if !ok {
				t.Fatal("expected parse success")
			}
			if strings.Join(status.NodeList, ",") != strings.Join(tc.wantNodeList, ",") {
				t.Fatalf("unexpected node list: %#v", status.NodeList)
			}
		})
	}
}

func TestParseDFOutput(t *testing.T) {
	filesystems, err := parseDFOutput(strings.Join([]string{
		"Filesystem 1-blocks Used Available Use% Mounted on",
		"10.0.0.10@o2ib:/lustre 67070255194112 23089744183296 43980511010816 35% /mnt/sharefs/user44",
	}, "\n"))
	if err != nil {
		t.Fatalf("parse df output: %v", err)
	}
	if len(filesystems) != 1 {
		t.Fatalf("expected one filesystem, got %d", len(filesystems))
	}
	if filesystems[0].Target != "/mnt/sharefs/user44" || filesystems[0].UsePercent != 35 {
		t.Fatalf("unexpected filesystem payload: %#v", filesystems[0])
	}
}

func TestParseTopologyOutput(t *testing.T) {
	entry, err := parseTopologyOutput("", "us-west-a2-gpu-014\n---\nGPU0\tGPU1\nGPU0\tX\tNV18\n")
	if err != nil {
		t.Fatalf("parse topology output: %v", err)
	}
	if entry.Node != "us-west-a2-gpu-014" {
		t.Fatalf("unexpected node: %#v", entry)
	}
	if !strings.Contains(entry.Matrix, "NV18") {
		t.Fatalf("expected topology matrix content, got %q", entry.Matrix)
	}
}

func TestProbeEphemeralTopologyUsesSrun(t *testing.T) {
	command := fmt.Sprintf(
		"bash -lc srun --immediate=60 --gres=gpu:8 --cpus-per-task=16 --mem=102400M --time=%s --ntasks=1 bash -lc %s",
		shellQuote("00:05:00"),
		shellQuote(topologyProbeScript()),
	)
	runner := &fakeRunner{
		outputs: map[string][]byte{
			command: []byte("us-west-a2-gpu-014\n---\nGPU0\tGPU1\nGPU0\tX\tNV18\n"),
		},
	}
	adapter := New(runner, "")
	probe, err := adapter.ProbeTopology(context.Background(), domain.TopologyRequest{})
	if err != nil {
		t.Fatalf("probe topology: %v", err)
	}
	if probe.Mode != string(TopologyProbeEphemeral) || probe.RequestedGPUs != 8 {
		t.Fatalf("unexpected probe payload: %#v", probe)
	}
	if len(runner.calls) != 1 || runner.calls[0] != command {
		t.Fatalf("unexpected ephemeral command: %#v", runner.calls)
	}
}

func TestReadLogLocalFullFile(t *testing.T) {
	path := writeTempFile(t, "line-1\nline-2\n")
	runner := &fakeRunner{}
	adapter := New(runner, "")
	got, err := adapter.ReadLog(context.Background(), path, 0)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if string(got) != "line-1\nline-2\n" {
		t.Fatalf("unexpected content: %q", string(got))
	}
	if len(runner.calls) != 0 {
		t.Fatalf("expected local log read to avoid runner, got %#v", runner.calls)
	}
}

func TestReadLogLocalTail(t *testing.T) {
	path := writeTempFile(t, "line-1\nline-2\nline-3\n")
	adapter := New(&fakeRunner{}, "")
	got, err := adapter.ReadLog(context.Background(), path, 2)
	if err != nil {
		t.Fatalf("read log tail: %v", err)
	}
	if string(got) != "line-2\nline-3\n" {
		t.Fatalf("unexpected tail content: %q", string(got))
	}
}

func TestReadLogRemoteTailUsesSSH(t *testing.T) {
	logPath := "/mnt/sharefs/user44/.fuse/artifacts/demo job/demo-101.out"
	command := fmt.Sprintf("ssh user44@184.34.82.180 %s", fmt.Sprintf("bash -lc %s", shellQuote(fmt.Sprintf("tail -n 25 -- %s", shellQuote(logPath)))))
	runner := &fakeRunner{
		outputs: map[string][]byte{
			command: []byte("remote-lines\n"),
		},
	}
	adapter := New(runner, "user44@184.34.82.180")
	got, err := adapter.ReadLog(context.Background(), logPath, 25)
	if err != nil {
		t.Fatalf("read remote log: %v", err)
	}
	if string(got) != "remote-lines\n" {
		t.Fatalf("unexpected remote content: %q", string(got))
	}
	if len(runner.calls) != 1 || runner.calls[0] != command {
		t.Fatalf("unexpected remote command: %#v", runner.calls)
	}
}

func TestReadLogRemoteErrorIncludesContext(t *testing.T) {
	logPath := "/mnt/sharefs/user44/.fuse/artifacts/demo's/demo-101.err"
	runner := &fakeRunner{err: errors.New("boom")}
	adapter := New(runner, "user44@184.34.82.180")
	_, err := adapter.ReadLog(context.Background(), logPath, 0)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "remote log read failed") {
		t.Fatalf("expected wrapped error, got %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("expected one remote call, got %#v", runner.calls)
	}
	expectedFragment := fmt.Sprintf("bash -lc %s", shellQuote(fmt.Sprintf("cat -- %s", shellQuote(logPath))))
	if !strings.Contains(runner.calls[0], expectedFragment) {
		t.Fatalf("expected quoted path, got %q", runner.calls[0])
	}
}

func writeTempFile(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "log.txt")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}
