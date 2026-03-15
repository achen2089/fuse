package recipes

import (
	"strings"
	"testing"
)

func TestBuildTrainSpecForMakemore(t *testing.T) {
	spec, err := BuildTrainSpec(TrainInput{
		Name:    "makemore-demo",
		Team:    "default",
		Example: "makemore",
	})
	if err != nil {
		t.Fatalf("build train spec: %v", err)
	}
	if spec.Type != "train" || spec.GPUs != 1 {
		t.Fatalf("unexpected spec: %#v", spec)
	}
	if spec.ContainerImage != DefaultMakemoreImage {
		t.Fatalf("unexpected image: %#v", spec)
	}
	if spec.CommandOrRecipe == "" || spec.CommandOrRecipe[:6] != "python" {
		t.Fatalf("unexpected command: %s", spec.CommandOrRecipe)
	}
}

func TestBuildTrainSpecForNanochatUsesTorchrun(t *testing.T) {
	spec, err := BuildTrainSpec(TrainInput{
		Name:    "nanochat-demo",
		Team:    "default",
		Example: "nanochat",
		GPUs:    4,
	})
	if err != nil {
		t.Fatalf("build train spec: %v", err)
	}
	if spec.GPUs != 4 || spec.Env["OMP_NUM_THREADS"] != "1" {
		t.Fatalf("unexpected spec: %#v", spec)
	}
	if spec.CommandOrRecipe[:8] != "torchrun" {
		t.Fatalf("expected torchrun command, got %s", spec.CommandOrRecipe)
	}
}

func TestBuildTrainSpecForMultiNodeNanochatUsesSrunLauncher(t *testing.T) {
	spec, err := BuildTrainSpec(TrainInput{
		Name:    "nanochat-demo",
		Team:    "default",
		Example: "nanochat",
		GPUs:    16,
	})
	if err != nil {
		t.Fatalf("build train spec: %v", err)
	}
	if spec.Nodes != 2 || spec.Tasks != 2 || spec.TasksPerNode != 1 || spec.GPUsPerNode != 8 {
		t.Fatalf("unexpected multinode layout: %#v", spec)
	}
	if spec.ContainerImage != "" {
		t.Fatalf("expected host-side launcher for multinode run, got image %q", spec.ContainerImage)
	}
	if !containsAll(spec.CommandOrRecipe, `srun --ntasks=2 --ntasks-per-node=1`, `--container-image='/mnt/sharefs/user44/fuse-ngc-pytorch-2502.sqsh'`, `torchrun --nnodes=2`, `--nproc-per-node=8`) {
		t.Fatalf("unexpected multinode command: %s", spec.CommandOrRecipe)
	}
}

func TestBuildTrainSpecWrapsHoldAndSupportsAxolotlProbe(t *testing.T) {
	spec, err := BuildTrainSpec(TrainInput{
		Name:        "axolotl-demo",
		Team:        "default",
		Example:     "axolotl-probe",
		HoldSeconds: 120,
	})
	if err != nil {
		t.Fatalf("build axolotl train spec: %v", err)
	}
	if spec.GPUs != 1 || spec.ContainerImage != DefaultNanochatImage {
		t.Fatalf("unexpected spec: %#v", spec)
	}
	if spec.CommandOrRecipe[:8] != "bash -lc" {
		t.Fatalf("expected hold wrapper, got %s", spec.CommandOrRecipe)
	}
	if got := spec.CommandOrRecipe; got == "" || !containsAll(got, "axolotl_probe.py", "FUSE_HOLD_SECONDS=120", "sleep 120") {
		t.Fatalf("unexpected hold command: %s", got)
	}
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
