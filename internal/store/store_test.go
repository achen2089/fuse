package store

import (
	"context"
	"path/filepath"
	"testing"

	"fuse/internal/domain"
)

func TestUpdateJobStateReturnsFalseForIdenticalState(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	job := domain.JobFromSpec(domain.JobSpec{
		ID:              "job-1",
		Name:            "job-1",
		Team:            "default",
		Type:            domain.JobTypeRun,
		CommandOrRecipe: "/bin/true",
		Workdir:         ".",
		Env:             map[string]string{},
		GPUs:            1,
		CPUs:            1,
		MemoryMB:        1024,
		Walltime:        "00:01:00",
		CheckpointMode:  domain.CheckpointNone,
		PriorityHint:    domain.PriorityNormal,
		TopologyHint:    domain.TopologyAny,
		ArtifactsDir:    ".fuse/artifacts/job-1",
	})
	if err := st.CreateJob(ctx, job, domain.Allocation{JobID: job.ID}); err != nil {
		t.Fatalf("create job: %v", err)
	}

	why := domain.Why{
		ReasonCode: domain.ReasonScheduled,
		Summary:    "job is running",
		Detail:     "Slurm started the job successfully",
		RawState:   "RUNNING",
	}
	changed, err := st.UpdateJobState(ctx, job.ID, domain.JobStateRunning, "RUNNING", why)
	if err != nil {
		t.Fatalf("first update: %v", err)
	}
	if !changed {
		t.Fatal("expected first update to report a change")
	}

	changed, err = st.UpdateJobState(ctx, job.ID, domain.JobStateRunning, "RUNNING", why)
	if err != nil {
		t.Fatalf("second update: %v", err)
	}
	if changed {
		t.Fatal("expected identical update to report no change")
	}
}
