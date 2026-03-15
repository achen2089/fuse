package recipes

import (
	"fmt"
	"path/filepath"

	"fuse/internal/domain"
)

type FinetuneInput struct {
	Name         string
	Team         string
	Model        string
	Dataset      string
	OutputDir    string
	Workdir      string
	GPUs         int
	Walltime     string
	ArtifactsDir string
}

func BuildFinetuneSpec(in FinetuneInput) domain.JobSpec {
	outputDir := in.OutputDir
	if outputDir == "" {
		outputDir = filepath.Join(in.Workdir, "checkpoints", in.Name)
	}
	command := fmt.Sprintf(
		`python finetune.py --model_name %q --dataset %q --output_dir %q --bf16 --lora_r 16 --lora_alpha 32`,
		in.Model, in.Dataset, outputDir,
	)
	return domain.JobSpec{
		ID:              "",
		Name:            in.Name,
		Team:            in.Team,
		Type:            domain.JobTypeFinetune,
		CommandOrRecipe: command,
		Workdir:         in.Workdir,
		Env:             map[string]string{"TOKENIZERS_PARALLELISM": "false"},
		GPUs:            in.GPUs,
		CPUs:            4,
		MemoryMB:        64 * 1024,
		Walltime:        in.Walltime,
		CheckpointMode:  domain.CheckpointFilesystem,
		CheckpointDir:   outputDir,
		ResumeCommand:   fmt.Sprintf(`python finetune.py --resume_from_checkpoint %q`, outputDir),
		TopologyHint:    domain.TopologySameNode,
		ArtifactsDir:    in.ArtifactsDir,
	}
}
