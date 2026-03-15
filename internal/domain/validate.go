package domain

import (
	"errors"
	"fmt"
	"strings"
)

func (s *JobSpec) Normalize() {
	if s.ID == "" {
		s.ID = strings.ToLower(strings.ReplaceAll(s.Name, " ", "-"))
	}
	if s.Team == "" {
		s.Team = "default"
	}
	if s.Type == "" {
		s.Type = JobTypeRun
	}
	if s.TopologyHint == "" {
		s.TopologyHint = TopologyAny
	}
	if s.PriorityHint == "" {
		s.PriorityHint = PriorityNormal
	}
	if s.CheckpointMode == "" {
		s.CheckpointMode = CheckpointNone
	}
	if s.Env == nil {
		s.Env = map[string]string{}
	}
}

func (s JobSpec) Validate() error {
	if s.Name == "" {
		return errors.New("job name is required")
	}
	if s.Type != JobTypeRun && s.Type != JobTypeTrain && s.Type != JobTypeFinetune {
		return fmt.Errorf("unsupported job type %q", s.Type)
	}
	if s.GPUs <= 0 {
		return errors.New("gpus must be greater than zero")
	}
	if s.MemoryMB < 0 || s.CPUs < 0 || s.Nodes < 0 || s.Tasks < 0 || s.TasksPerNode < 0 || s.GPUsPerNode < 0 {
		return errors.New("cpus, memory, nodes, tasks, and per-node counts must be non-negative")
	}
	if s.CommandOrRecipe == "" {
		return errors.New("command_or_recipe is required")
	}
	if s.Nodes > 0 && s.GPUsPerNode > 0 && s.Nodes*s.GPUsPerNode != s.GPUs {
		return errors.New("nodes * gpus_per_node must equal total gpus")
	}
	if s.Nodes > 0 && s.Tasks > 0 && s.TasksPerNode > 0 && s.Nodes*s.TasksPerNode != s.Tasks {
		return errors.New("nodes * tasks_per_node must equal total tasks")
	}
	if s.ContainerImage == "" && (len(s.ContainerMounts) > 0 || s.ContainerWorkdir != "" || s.ContainerMountHome) {
		return errors.New("container settings require container_image")
	}
	if s.CheckpointMode == CheckpointFilesystem && s.CheckpointDir == "" {
		return errors.New("checkpoint_dir is required for filesystem checkpoints")
	}
	if s.ResumeCommand != "" && s.CheckpointMode == CheckpointNone {
		return errors.New("resume_command requires checkpoint_mode=filesystem")
	}
	if s.Type == JobTypeFinetune && s.CommandOrRecipe == "" {
		return errors.New("finetune jobs require a recipe")
	}
	return nil
}
