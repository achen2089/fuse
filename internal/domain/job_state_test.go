package domain

import "testing"

func TestJobStateCanTransitionToAllowsRepeatedObservedState(t *testing.T) {
	if !JobStateRunning.CanTransitionTo(JobStateRunning) {
		t.Fatal("expected RUNNING -> RUNNING to be allowed")
	}
	if !JobStateSucceeded.CanTransitionTo(JobStateSucceeded) {
		t.Fatal("expected SUCCEEDED -> SUCCEEDED to be allowed")
	}
}

func TestJobStateCanTransitionToAllowsDirectSlurmTerminalShortcuts(t *testing.T) {
	cases := []struct {
		from JobState
		to   JobState
	}{
		{from: JobStateSubmitting, to: JobStateRunning},
		{from: JobStateSubmitting, to: JobStateSucceeded},
		{from: JobStatePending, to: JobStateSucceeded},
		{from: JobStatePending, to: JobStateCancelled},
		{from: JobStateRunning, to: JobStateSucceeded},
		{from: JobStateRunning, to: JobStateCancelled},
	}
	for _, tc := range cases {
		if !tc.from.CanTransitionTo(tc.to) {
			t.Fatalf("expected %s -> %s to be allowed", tc.from, tc.to)
		}
	}
}
