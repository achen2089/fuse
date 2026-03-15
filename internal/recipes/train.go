package recipes

import (
	"fmt"
	"path"
	"strings"

	"fuse/internal/domain"
)

const (
	DefaultSharedRoot    = "/mnt/sharefs/user44"
	DefaultWorkloadRoot  = "/mnt/sharefs/user44/fuse-workloads"
	DefaultMakemoreImage = "docker://pytorch/pytorch:2.7.0-cuda12.8-cudnn9-runtime"
	DefaultNanochatImage = "/mnt/sharefs/user44/fuse-ngc-pytorch-2502.sqsh"
)

type TrainInput struct {
	Name         string
	Team         string
	Example      string
	GPUs         int
	CPUs         int
	MemoryMB     int64
	Walltime     string
	Image        string
	SharedRoot   string
	WorkloadRoot string
	Steps        int
	ArtifactsDir string
	HoldSeconds  int
	MountHome    bool
	Env          map[string]string
}

func BuildTrainSpec(in TrainInput) (domain.JobSpec, error) {
	example := strings.ToLower(strings.TrimSpace(in.Example))
	if example == "" {
		example = "makemore"
	}
	if in.Name == "" {
		in.Name = example + "-train"
	}
	if in.Team == "" {
		in.Team = "default"
	}
	if in.SharedRoot == "" {
		in.SharedRoot = DefaultSharedRoot
	}
	if in.WorkloadRoot == "" {
		in.WorkloadRoot = DefaultWorkloadRoot
	}
	if len(in.Env) == 0 {
		in.Env = map[string]string{}
	}
	in.Env["PYTHONUNBUFFERED"] = "1"

	spec := domain.JobSpec{
		Name:               in.Name,
		Team:               in.Team,
		Type:               domain.JobTypeTrain,
		Workdir:            in.SharedRoot,
		ContainerMounts:    []string{fmt.Sprintf("%s:%s", in.SharedRoot, in.SharedRoot)},
		ContainerWorkdir:   in.SharedRoot,
		ContainerMountHome: in.MountHome,
		Env:                in.Env,
		CheckpointMode:     domain.CheckpointNone,
		ArtifactsDir:       in.ArtifactsDir,
		TopologyHint:       domain.TopologySameNode,
	}

	switch example {
	case "makemore":
		if in.GPUs <= 0 {
			in.GPUs = 1
		}
		if in.CPUs <= 0 {
			in.CPUs = 4
		}
		if in.MemoryMB <= 0 {
			in.MemoryMB = 16 * 1024
		}
		if in.Walltime == "" {
			in.Walltime = "00:05:00"
		}
		if in.Image == "" {
			in.Image = DefaultMakemoreImage
		}
		if in.Steps <= 0 {
			in.Steps = 200
		}
		spec.GPUs = in.GPUs
		spec.CPUs = in.CPUs
		spec.MemoryMB = in.MemoryMB
		spec.Walltime = in.Walltime
		spec.ContainerImage = in.Image
		spec.CommandOrRecipe = wrapForHold(
			fmt.Sprintf("python %s --steps %d", shellQuote(path.Join(in.WorkloadRoot, "makemore_smoke.py")), in.Steps),
			in.HoldSeconds,
		)
		return spec, nil
	case "nanochat":
		if in.GPUs <= 0 {
			in.GPUs = 1
		}
		if in.GPUs > 8 {
			return domain.JobSpec{}, fmt.Errorf("fuse train --example nanochat currently supports a single node up to 8 GPUs; use fuse shard plus the manual Slurm path for multi-node runs")
		}
		if in.CPUs <= 0 {
			in.CPUs = 4 * in.GPUs
		}
		if in.MemoryMB <= 0 {
			in.MemoryMB = int64(20 * 1024 * maxInt(1, in.GPUs))
		}
		if in.Walltime == "" {
			in.Walltime = "00:10:00"
		}
		if in.Image == "" {
			in.Image = DefaultNanochatImage
		}
		if in.Steps <= 0 {
			in.Steps = 40
		}
		if in.GPUs > 1 {
			in.Env["OMP_NUM_THREADS"] = "1"
		}
		spec.GPUs = in.GPUs
		spec.CPUs = in.CPUs
		spec.MemoryMB = in.MemoryMB
		spec.Walltime = in.Walltime
		spec.ContainerImage = in.Image
		if in.GPUs == 1 {
			spec.CommandOrRecipe = wrapForHold(
				fmt.Sprintf("python %s --steps %d", shellQuote(path.Join(in.WorkloadRoot, "nanochat_smoke.py")), in.Steps),
				in.HoldSeconds,
			)
		} else {
			spec.CommandOrRecipe = wrapForHold(
				fmt.Sprintf("torchrun --standalone --nnodes=1 --nproc-per-node=%d %s --steps %d", in.GPUs, shellQuote(path.Join(in.WorkloadRoot, "nanochat_smoke.py")), in.Steps),
				in.HoldSeconds,
			)
		}
		return spec, nil
	case "axolotl", "axolotl-probe":
		if in.GPUs <= 0 {
			in.GPUs = 1
		}
		if in.CPUs <= 0 {
			in.CPUs = 4
		}
		if in.MemoryMB <= 0 {
			in.MemoryMB = 16 * 1024
		}
		if in.Walltime == "" {
			in.Walltime = "00:05:00"
		}
		if in.Image == "" {
			in.Image = DefaultNanochatImage
		}
		spec.GPUs = in.GPUs
		spec.CPUs = in.CPUs
		spec.MemoryMB = in.MemoryMB
		spec.Walltime = in.Walltime
		spec.ContainerImage = in.Image
		spec.CommandOrRecipe = wrapForHold(
			fmt.Sprintf("python %s", shellQuote(path.Join(in.WorkloadRoot, "axolotl_probe.py"))),
			in.HoldSeconds,
		)
		return spec, nil
	default:
		return domain.JobSpec{}, fmt.Errorf("unsupported train example %q (supported: makemore, nanochat, axolotl-probe)", in.Example)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func wrapForHold(command string, holdSeconds int) string {
	if holdSeconds <= 0 {
		return command
	}
	return fmt.Sprintf(
		"bash -lc %q",
		fmt.Sprintf("%s; printf 'FUSE_HOLD_SECONDS=%d\\n'; sleep %d", command, holdSeconds, holdSeconds),
	)
}
