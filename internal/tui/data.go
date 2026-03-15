package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"fuse/internal/domain"
)

type snapshot struct {
	CapturedAt time.Time
	Status     domain.ClusterStatus
	Nodes      []nodeSummary
	Jobs       []jobSummary
	Events     []eventSummary
}

type nodeSummary struct {
	Name        string
	SwitchName  string
	State       string
	Health      domain.HealthStatus
	TotalGPUs   int
	Allocated   int
	Free        int
	MaxTempC    int
	AverageUtil int
	Real        bool
	DeviceCount int
}

type jobSummary struct {
	ID          string
	Name        string
	Team        string
	State       domain.JobState
	SlurmID     string
	NodeSummary string
	RawState    string
	GPUs        int
	UpdatedAt   time.Time
}

type eventSummary struct {
	Time    time.Time
	Reason  domain.ReasonCode
	Summary string
}

type snapshotResultMsg struct {
	snapshot    snapshot
	attemptedAt time.Time
	err         error
}

func collectSnapshot(ctx context.Context, cli Client, eventLimit int) (snapshot, error) {
	var (
		status   domain.ClusterStatus
		nodes    []domain.Node
		devices  []domain.Device
		jobs     []domain.Job
		events   []domain.Event
		firstErr error
		errMu    sync.Mutex
		wg       sync.WaitGroup
	)

	setErr := func(err error) {
		if err == nil {
			return
		}
		errMu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		errMu.Unlock()
	}

	wg.Add(4)

	go func() {
		defer wg.Done()
		var err error
		status, err = cli.Status(ctx)
		setErr(err)
	}()

	go func() {
		defer wg.Done()
		var err error
		nodes, devices, err = cli.Nodes(ctx)
		setErr(err)
	}()

	go func() {
		defer wg.Done()
		var err error
		jobs, err = cli.Jobs(ctx)
		setErr(err)
	}()

	go func() {
		defer wg.Done()
		var err error
		events, err = cli.Events(ctx, eventLimit)
		setErr(err)
	}()

	wg.Wait()
	if firstErr != nil {
		return snapshot{}, firstErr
	}
	return buildSnapshot(status, nodes, devices, jobs, events, time.Now().UTC()), nil
}

func buildSnapshot(status domain.ClusterStatus, nodes []domain.Node, devices []domain.Device, jobs []domain.Job, events []domain.Event, capturedAt time.Time) snapshot {
	deviceIndex := map[string][]domain.Device{}
	for _, device := range devices {
		deviceIndex[device.NodeID] = append(deviceIndex[device.NodeID], device)
	}

	nodeRows := make([]nodeSummary, 0, len(nodes))
	for _, node := range nodes {
		row := nodeSummary{
			Name:       node.Name,
			SwitchName: defaultText(node.SwitchName, "-"),
			State:      defaultText(node.ObservedState, "-"),
			Health:     node.Health,
			TotalGPUs:  node.TotalGPUs,
			Allocated:  node.AllocatedGPUs,
			Free:       node.FreeGPUs,
			Real:       node.Real,
		}
		devs := deviceIndex[node.ID]
		row.DeviceCount = len(devs)
		if row.TotalGPUs == 0 {
			row.TotalGPUs = len(devs)
		}
		var utilTotal int
		for _, device := range devs {
			if device.Benchmark.TemperatureC > row.MaxTempC {
				row.MaxTempC = device.Benchmark.TemperatureC
			}
			utilTotal += device.Benchmark.UtilizationPct
			if device.Real {
				row.Real = true
			}
		}
		if len(devs) > 0 {
			row.AverageUtil = utilTotal / len(devs)
		}
		nodeRows = append(nodeRows, row)
	}
	sort.Slice(nodeRows, func(i, j int) bool {
		if nodeRows[i].Name == nodeRows[j].Name {
			return nodeRows[i].SwitchName < nodeRows[j].SwitchName
		}
		return nodeRows[i].Name < nodeRows[j].Name
	})

	jobRows := make([]jobSummary, 0, len(jobs))
	for _, job := range jobs {
		jobRows = append(jobRows, jobSummary{
			ID:          job.ID,
			Name:        defaultText(job.Name, job.ID),
			Team:        defaultText(job.Team, "-"),
			State:       job.State,
			SlurmID:     defaultText(job.SlurmJobID, "-"),
			NodeSummary: defaultText(strings.Join(job.NodeList, ","), "-"),
			RawState:    defaultText(job.RawState, "-"),
			GPUs:        job.GPUs,
			UpdatedAt:   latestTime(job.UpdatedAt, job.CreatedAt),
		})
	}
	sort.Slice(jobRows, func(i, j int) bool {
		if jobRows[i].UpdatedAt.Equal(jobRows[j].UpdatedAt) {
			return jobRows[i].Name < jobRows[j].Name
		}
		return jobRows[i].UpdatedAt.After(jobRows[j].UpdatedAt)
	})

	eventRows := make([]eventSummary, 0, len(events))
	for _, event := range events {
		eventRows = append(eventRows, eventSummary{
			Time:    event.CreatedAt,
			Reason:  event.ReasonCode,
			Summary: defaultText(event.Summary, fmt.Sprintf("%s %s", event.ResourceType, event.ResourceID)),
		})
	}
	sort.Slice(eventRows, func(i, j int) bool {
		if eventRows[i].Time.Equal(eventRows[j].Time) {
			return string(eventRows[i].Reason) < string(eventRows[j].Reason)
		}
		return eventRows[i].Time.After(eventRows[j].Time)
	})

	return snapshot{
		CapturedAt: capturedAt,
		Status:     status,
		Nodes:      nodeRows,
		Jobs:       jobRows,
		Events:     eventRows,
	}
}

func latestTime(values ...time.Time) time.Time {
	var latest time.Time
	for _, value := range values {
		if value.After(latest) {
			latest = value
		}
	}
	return latest
}

func defaultText(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
