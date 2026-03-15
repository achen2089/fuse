package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"fuse/internal/domain"
)

func TestBuildSnapshotSortsAndAggregates(t *testing.T) {
	now := time.Date(2026, 3, 15, 15, 30, 0, 0, time.UTC)
	snap := buildSnapshot(
		domain.ClusterStatus{Nodes: 2, Devices: 4, Allocated: 2, Idle: 2, RunningJobs: 1, PendingJobs: 1},
		[]domain.Node{
			{ID: "n2", Name: "n2", SwitchName: "leaf-02", TotalGPUs: 2, AllocatedGPUs: 0, FreeGPUs: 2, Health: domain.HealthHealthy, ObservedState: "idle"},
			{ID: "n1", Name: "n1", SwitchName: "leaf-01", AllocatedGPUs: 2, FreeGPUs: 0, Health: domain.HealthDegraded, ObservedState: "mixed"},
		},
		[]domain.Device{
			{NodeID: "n1", Real: true, Benchmark: domain.Benchmark{TemperatureC: 48, UtilizationPct: 80}},
			{NodeID: "n1", Benchmark: domain.Benchmark{TemperatureC: 42, UtilizationPct: 40}},
			{NodeID: "n2", Benchmark: domain.Benchmark{TemperatureC: 35, UtilizationPct: 10}},
			{NodeID: "n2", Benchmark: domain.Benchmark{TemperatureC: 36, UtilizationPct: 20}},
		},
		[]domain.Job{
			{Name: "older", State: domain.JobStatePending, UpdatedAt: now.Add(-2 * time.Minute)},
			{Name: "newer", State: domain.JobStateRunning, UpdatedAt: now.Add(-1 * time.Minute)},
		},
		[]domain.Event{
			{CreatedAt: now.Add(-5 * time.Minute), ReasonCode: domain.ReasonUnknown, Summary: "older"},
			{CreatedAt: now.Add(-1 * time.Minute), ReasonCode: domain.ReasonScheduled, Summary: "newer"},
		},
		now,
	)

	if got, want := len(snap.Nodes), 2; got != want {
		t.Fatalf("nodes len = %d, want %d", got, want)
	}
	if snap.Nodes[0].Name != "n1" || snap.Nodes[1].Name != "n2" {
		t.Fatalf("nodes not sorted by name: %#v", snap.Nodes)
	}
	if snap.Nodes[0].TotalGPUs != 2 {
		t.Fatalf("n1 total GPUs = %d, want 2", snap.Nodes[0].TotalGPUs)
	}
	if snap.Nodes[0].MaxTempC != 48 || snap.Nodes[0].AverageUtil != 60 {
		t.Fatalf("unexpected node aggregate: %#v", snap.Nodes[0])
	}
	if !snap.Nodes[0].Real {
		t.Fatalf("expected real device marker to propagate")
	}

	if got := snap.Jobs[0].Name; got != "newer" {
		t.Fatalf("jobs not sorted by updated desc: first=%q", got)
	}
	if got := snap.Events[0].Summary; got != "newer" {
		t.Fatalf("events not sorted by created desc: first=%q", got)
	}
}

func TestModelUpdateCycleLoadsSnapshot(t *testing.T) {
	client := &fakeTUIClient{
		status: domain.ClusterStatus{Nodes: 1, Devices: 2, Allocated: 2, Idle: 0, RunningJobs: 1},
		nodes: []domain.Node{
			{ID: "n1", Name: "n1", SwitchName: "leaf-01", TotalGPUs: 2, AllocatedGPUs: 2, FreeGPUs: 0, Health: domain.HealthHealthy, ObservedState: "mixed"},
		},
		devices: []domain.Device{
			{NodeID: "n1", Benchmark: domain.Benchmark{TemperatureC: 45, UtilizationPct: 90}},
			{NodeID: "n1", Benchmark: domain.Benchmark{TemperatureC: 46, UtilizationPct: 80}},
		},
		jobs: []domain.Job{
			{Name: "train", Team: "default", GPUs: 2, State: domain.JobStateRunning, UpdatedAt: time.Now().UTC()},
		},
		events: []domain.Event{
			{CreatedAt: time.Now().UTC(), ReasonCode: domain.ReasonScheduled, Summary: "job placed"},
		},
	}

	m := newModel(context.Background(), client, normalizeOptions(Options{EventLimit: 5}))
	m.width = 140
	m.height = 40
	m.layoutViewports()

	msg := fetchSnapshotCmd(context.Background(), client, 5)()
	result, ok := msg.(snapshotResultMsg)
	if !ok {
		t.Fatalf("fetchSnapshotCmd returned %T, want snapshotResultMsg", msg)
	}

	updated, cmd := m.Update(result)
	got := updated.(*model)
	if !got.hasSnapshot {
		t.Fatalf("expected snapshot to be loaded")
	}
	if got.loading || got.refreshing {
		t.Fatalf("expected steady state after snapshot load: loading=%t refreshing=%t", got.loading, got.refreshing)
	}
	if got.lastErr != nil {
		t.Fatalf("unexpected lastErr: %v", got.lastErr)
	}
	if got.nodesViewport.TotalLineCount() == 0 || got.jobsViewport.TotalLineCount() == 0 || got.eventsViewport.TotalLineCount() == 0 {
		t.Fatalf("expected viewports to be populated")
	}
	if cmd == nil {
		t.Fatalf("expected automatic refresh command after successful load")
	}
}

func TestModelPreservesLastGoodSnapshotOnRefreshFailure(t *testing.T) {
	m := newModel(context.Background(), &fakeTUIClient{}, normalizeOptions(Options{}))
	m.width = 120
	m.height = 32
	m.data = snapshot{
		CapturedAt: time.Date(2026, 3, 15, 15, 45, 0, 0, time.UTC),
		Status:     domain.ClusterStatus{Nodes: 1},
		Nodes:      []nodeSummary{{Name: "n1", TotalGPUs: 8}},
	}
	m.hasSnapshot = true
	m.layoutViewports()
	m.syncViewportContent()

	updated, _ := m.Update(snapshotResultMsg{
		attemptedAt: time.Date(2026, 3, 15, 15, 46, 0, 0, time.UTC),
		err:         errors.New("backend unavailable"),
	})
	got := updated.(*model)

	if !got.hasSnapshot {
		t.Fatalf("expected last good snapshot to remain available")
	}
	if got.data.CapturedAt.IsZero() {
		t.Fatalf("expected prior snapshot to remain intact")
	}
	if got.lastErr == nil || !strings.Contains(got.lastErr.Error(), "backend unavailable") {
		t.Fatalf("expected refresh error to be surfaced, got %v", got.lastErr)
	}
	if got.loading || got.refreshing {
		t.Fatalf("expected failure to leave app idle: loading=%t refreshing=%t", got.loading, got.refreshing)
	}
}

func TestModelTabCyclesFocusedPane(t *testing.T) {
	m := newModel(context.Background(), &fakeTUIClient{}, normalizeOptions(Options{}))
	if m.selected != nodesPane {
		t.Fatalf("initial selected pane = %v, want nodesPane", m.selected)
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	got := updated.(*model)
	if got.selected != jobsPane {
		t.Fatalf("after tab selected = %v, want jobsPane", got.selected)
	}

	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	got = updated.(*model)
	if got.selected != nodesPane {
		t.Fatalf("after shift+tab selected = %v, want nodesPane", got.selected)
	}
}

func TestCommandModeFocusesPane(t *testing.T) {
	m := newModel(context.Background(), &fakeTUIClient{}, normalizeOptions(Options{}))
	m.openCommandMode("jobs")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(*model)
	if got.commandMode {
		t.Fatalf("expected command mode to close after submit")
	}
	if got.selected != jobsPane {
		t.Fatalf("expected jobs pane after command, got %v", got.selected)
	}
	if got.commandStatus == "" || !strings.Contains(strings.ToLower(got.commandStatus), "jobs") {
		t.Fatalf("expected command status to mention jobs, got %q", got.commandStatus)
	}
	if cmd != nil {
		t.Fatalf("did not expect pane focus command to return a follow-up cmd")
	}
}

func TestCommandRefreshStartsFetch(t *testing.T) {
	client := &fakeTUIClient{}
	m := newModel(context.Background(), client, normalizeOptions(Options{}))
	m.loading = false
	m.refreshing = false
	m.openCommandMode("refresh")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(*model)
	if !got.refreshing {
		t.Fatalf("expected refresh command to mark model refreshing")
	}
	if cmd == nil {
		t.Fatalf("expected refresh command to return fetch cmd")
	}
}

func TestCommandBarHintIsVisibleWhenIdle(t *testing.T) {
	m := newModel(context.Background(), &fakeTUIClient{}, normalizeOptions(Options{}))
	m.width = 120
	m.height = 30

	bar := m.renderCommandBar()
	if !strings.Contains(bar, "Press : for commands") {
		t.Fatalf("expected idle command bar hint, got %q", bar)
	}
}

func TestViewShowsSmallTerminalFallback(t *testing.T) {
	m := newModel(context.Background(), &fakeTUIClient{}, normalizeOptions(Options{}))
	m.width = 40
	m.height = 10

	view := m.View()
	if !strings.Contains(view, "needs a little more room") {
		t.Fatalf("expected small terminal fallback, got %q", view)
	}
}

type fakeTUIClient struct {
	status  domain.ClusterStatus
	nodes   []domain.Node
	devices []domain.Device
	jobs    []domain.Job
	events  []domain.Event
	err     error
}

func (f *fakeTUIClient) Status(context.Context) (domain.ClusterStatus, error) {
	return f.status, f.err
}

func (f *fakeTUIClient) Nodes(context.Context) ([]domain.Node, []domain.Device, error) {
	return f.nodes, f.devices, f.err
}

func (f *fakeTUIClient) Jobs(context.Context) ([]domain.Job, error) {
	return f.jobs, f.err
}

func (f *fakeTUIClient) Events(context.Context, int) ([]domain.Event, error) {
	return f.events, f.err
}
