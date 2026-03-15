package domain

import "testing"

func TestJobSpecValidateRejectsContainerSettingsWithoutImage(t *testing.T) {
	spec := JobSpec{
		Name:               "bad-container-spec",
		Type:               JobTypeRun,
		CommandOrRecipe:    "/bin/true",
		GPUs:               1,
		CPUs:               1,
		MemoryMB:           1024,
		Walltime:           "00:01:00",
		ContainerMountHome: true,
	}
	if err := spec.Validate(); err == nil {
		t.Fatal("expected container validation error")
	}
}

func TestJobSpecValidateAllowsContainerSettingsWithImage(t *testing.T) {
	spec := JobSpec{
		Name:               "good-container-spec",
		Type:               JobTypeRun,
		CommandOrRecipe:    "/bin/true",
		GPUs:               1,
		CPUs:               1,
		MemoryMB:           1024,
		Walltime:           "00:01:00",
		ContainerImage:     "/mnt/sharefs/user44/fuse-ngc-pytorch-2502.sqsh",
		ContainerMountHome: true,
	}
	if err := spec.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}
