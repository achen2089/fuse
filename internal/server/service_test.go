package server

import (
	"testing"

	"fuse/internal/domain"
	"fuse/internal/slurm"
)

func TestNormalizeStatusTreatsCancelledByAsCancelled(t *testing.T) {
	state, why := normalizeStatus(slurm.JobStatus{State: "CANCELLED by 2035"})
	if state != domain.JobStateCancelled {
		t.Fatalf("state = %s, want %s", state, domain.JobStateCancelled)
	}
	if why.ReasonCode != domain.ReasonExternalCancellation {
		t.Fatalf("reason = %s, want %s", why.ReasonCode, domain.ReasonExternalCancellation)
	}
	if why.RawState != "CANCELLED BY 2035" {
		t.Fatalf("raw state = %q, want %q", why.RawState, "CANCELLED BY 2035")
	}
}

func TestReconcileOutcomeKeepsCancellingWhileSlurmStillRuns(t *testing.T) {
	state, why := reconcileOutcome(domain.Job{State: domain.JobStateCancelling}, slurm.JobStatus{
		SlurmJobID: "1234",
		State:      "RUNNING",
	})
	if state != domain.JobStateCancelling {
		t.Fatalf("state = %s, want %s", state, domain.JobStateCancelling)
	}
	if why.ReasonCode != domain.ReasonCancelledByOperator {
		t.Fatalf("reason = %s, want %s", why.ReasonCode, domain.ReasonCancelledByOperator)
	}
	if why.RawState != "RUNNING" {
		t.Fatalf("raw state = %q, want RUNNING", why.RawState)
	}
}

func TestJobNeedsStateUpdateSkipsIdenticalPoll(t *testing.T) {
	job := domain.Job{
		State:         domain.JobStateRunning,
		RawState:      "RUNNING",
		ReasonCode:    domain.ReasonScheduled,
		ReasonSummary: "job is running",
		ReasonDetail:  "Slurm started the job successfully",
		Suggestions:   nil,
	}
	if jobNeedsStateUpdate(job, domain.JobStateRunning, domain.Why{
		ReasonCode: domain.ReasonScheduled,
		Summary:    "job is running",
		Detail:     "Slurm started the job successfully",
		RawState:   "RUNNING",
	}) {
		t.Fatal("expected identical reconcile poll to be skipped")
	}
}

func TestHydrateJobRuntimeSuppressesExitCodeUntilTerminal(t *testing.T) {
	job := domain.Job{State: domain.JobStateRunning}
	attempt := domain.JobAttempt{
		Attempt:    1,
		Executor:   "slurm",
		SlurmJobID: "1234",
		NodeList:   []string{"node-1"},
	}
	exitCode := 0
	attempt.ExitCode = &exitCode
	applyAttemptRuntime(&job, attempt)
	if job.ExitCode != nil {
		t.Fatalf("exit code = %v, want nil for non-terminal job", *job.ExitCode)
	}

	job.State = domain.JobStateSucceeded
	applyAttemptRuntime(&job, attempt)
	if job.ExitCode == nil || *job.ExitCode != 0 {
		t.Fatalf("exit code = %#v, want 0 for terminal job", job.ExitCode)
	}
}
