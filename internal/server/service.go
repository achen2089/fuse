package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fuse/internal/discovery"
	"fuse/internal/domain"
	"fuse/internal/planner"
	"fuse/internal/recipes"
	shardplan "fuse/internal/shard"
	"fuse/internal/sim"
	"fuse/internal/slurm"
	"fuse/internal/store"
)

type Config struct {
	DBPath         string
	Faker          bool
	WithNVML       bool
	SSHHost        string
	GuaranteedGPUs int
	ArtifactsDir   string
	PollInterval   time.Duration
}

type Service struct {
	store       *store.Store
	discoverers []discovery.Discoverer
	planner     *planner.Planner
	simulator   *sim.Simulator
	slurm       *slurm.Adapter
	events      *Broadcaster
	logger      *slog.Logger
	cfg         Config

	startOnce sync.Once
}

func New(ctx context.Context, cfg Config) (*Service, error) {
	if cfg.DBPath == "" {
		cfg.DBPath = filepath.Join(".fuse", "state.db")
	}
	if cfg.ArtifactsDir == "" {
		cfg.ArtifactsDir = filepath.Join(".fuse", "artifacts")
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 5 * time.Second
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.ArtifactsDir, 0o755); err != nil {
		return nil, err
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	svc := &Service{
		store:   st,
		planner: planner.New(),
		slurm:   slurm.New(nil, cfg.SSHHost),
		events:  NewBroadcaster(),
		logger:  slog.Default(),
		cfg:     cfg,
	}
	svc.simulator = sim.New(svc.planner)
	if cfg.SSHHost != "" {
		svc.discoverers = append(svc.discoverers, discovery.NewSlurm(cfg.SSHHost))
	}
	if cfg.Faker {
		svc.discoverers = append(svc.discoverers, discovery.NewFaker())
	}
	if cfg.WithNVML {
		svc.discoverers = append(svc.discoverers, discovery.NewNVML())
	}
	if len(svc.discoverers) == 0 {
		svc.discoverers = append(svc.discoverers, discovery.NewFaker())
	}
	if err := svc.refreshDiscovery(ctx); err != nil {
		return nil, err
	}
	if err := svc.ensureDefaultTeam(ctx); err != nil {
		return nil, err
	}
	return svc, nil
}

func (s *Service) Close() error {
	return s.store.Close()
}

func (s *Service) Start(ctx context.Context) {
	s.startOnce.Do(func() {
		ticker := time.NewTicker(s.cfg.PollInterval)
		go func() {
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := s.Reconcile(ctx); err != nil {
						s.logger.Warn("reconcile failed", "err", err)
					}
				}
			}
		}()
	})
}

func (s *Service) SubscribeEvents() chan domain.Event {
	return s.events.Subscribe()
}

func (s *Service) UnsubscribeEvents(ch chan domain.Event) {
	s.events.Unsubscribe(ch)
}

func (s *Service) refreshDiscovery(ctx context.Context) error {
	var merged discovery.Snapshot
	for _, discoverer := range s.discoverers {
		snapshot, err := discoverer.Discover(ctx)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				s.logger.Warn("discovery failed", "err", err)
			}
			continue
		}
		merged.Nodes = append(merged.Nodes, snapshot.Nodes...)
		merged.Devices = append(merged.Devices, snapshot.Devices...)
		merged.Links = append(merged.Links, snapshot.Links...)
	}
	if len(merged.Nodes) == 0 {
		return fmt.Errorf("no cluster nodes discovered")
	}
	return s.store.ReplaceClusterSnapshot(ctx, merged.Nodes, merged.Devices, merged.Links)
}

func (s *Service) ensureDefaultTeam(ctx context.Context) error {
	teams, err := s.store.ListTeams(ctx)
	if err != nil {
		return err
	}
	for _, team := range teams {
		if team.Name == "default" {
			return nil
		}
	}
	devices, err := s.store.ListDevices(ctx)
	if err != nil {
		return err
	}
	quota := len(devices)
	if s.cfg.GuaranteedGPUs > 0 && s.cfg.GuaranteedGPUs < quota {
		quota = s.cfg.GuaranteedGPUs
	}
	return s.store.UpsertTeam(ctx, domain.Team{
		Name:         "default",
		QuotaGPUs:    quota,
		BurstEnabled: true,
		CreatedAt:    time.Now().UTC(),
	})
}

func (s *Service) Status(ctx context.Context) (domain.ClusterStatus, error) {
	nodes, err := s.store.ListNodes(ctx)
	if err != nil {
		return domain.ClusterStatus{}, err
	}
	devices, err := s.store.ListDevices(ctx)
	if err != nil {
		return domain.ClusterStatus{}, err
	}
	jobs, err := s.store.ListJobs(ctx)
	if err != nil {
		return domain.ClusterStatus{}, err
	}
	allocations, err := s.store.ListAllocations(ctx)
	if err != nil {
		return domain.ClusterStatus{}, err
	}
	allocated := allocatedDeviceCount(allocations, jobs)
	if observed, ok := observedAllocatedDeviceCount(nodes); ok {
		allocated = observed
	}
	deviceCount := len(devices)
	if observedDevices := observedDeviceCount(nodes); observedDevices > 0 {
		deviceCount = observedDevices
	}
	status := domain.ClusterStatus{
		Nodes:     len(nodes),
		Devices:   deviceCount,
		Allocated: allocated,
		Idle:      maxInt(deviceCount-allocated, 0),
	}
	for _, job := range jobs {
		switch job.State {
		case domain.JobStateRunning:
			status.RunningJobs++
		case domain.JobStatePending, domain.JobStateSubmitting:
			status.PendingJobs++
		case domain.JobStateFailed:
			status.FailedJobs++
		}
	}
	return status, nil
}

func (s *Service) Nodes(ctx context.Context) ([]domain.Node, []domain.Device, error) {
	nodes, err := s.store.ListNodes(ctx)
	if err != nil {
		return nil, nil, err
	}
	devices, err := s.store.ListDevices(ctx)
	if err != nil {
		return nil, nil, err
	}
	return nodes, devices, nil
}

func (s *Service) Fabric(ctx context.Context) ([]domain.FabricLink, error) {
	return s.store.ListFabricLinks(ctx)
}

func (s *Service) Teams(ctx context.Context) ([]domain.Team, error) {
	return s.store.ListTeams(ctx)
}

func (s *Service) Jobs(ctx context.Context) ([]domain.Job, error) {
	jobs, err := s.store.ListJobs(ctx)
	if err != nil {
		return nil, err
	}
	return s.hydrateJobs(ctx, jobs)
}

func (s *Service) GetJob(ctx context.Context, jobID string) (domain.Job, error) {
	job, err := s.store.GetJob(ctx, jobID)
	if err != nil {
		return domain.Job{}, err
	}
	if err := s.hydrateJobRuntime(ctx, &job); err != nil {
		return domain.Job{}, err
	}
	return job, nil
}

func (s *Service) Logs(ctx context.Context, jobID, stream string, tailLines int) (domain.JobLog, error) {
	job, err := s.GetJob(ctx, jobID)
	if err != nil {
		return domain.JobLog{}, err
	}
	if job.SlurmJobID == "" {
		return domain.JobLog{}, fmt.Errorf("job %s has no Slurm attempt yet", jobID)
	}
	if stream == "" {
		stream = "stdout"
	}
	extension := ""
	switch stream {
	case "stdout":
		extension = "out"
	case "stderr":
		extension = "err"
	default:
		return domain.JobLog{}, fmt.Errorf("unsupported log stream %q", stream)
	}
	logPath := path.Join(job.ArtifactsDir, fmt.Sprintf("%s-%s.%s", job.Name, job.SlurmJobID, extension))
	content, err := s.slurm.ReadLog(ctx, logPath, tailLines)
	if err != nil {
		return domain.JobLog{}, err
	}
	return domain.JobLog{
		JobID:      job.ID,
		JobName:    job.Name,
		SlurmJobID: job.SlurmJobID,
		Stream:     stream,
		Path:       logPath,
		TailLines:  tailLines,
		Content:    string(content),
	}, nil
}

func (s *Service) Events(ctx context.Context, limit int) ([]domain.Event, error) {
	return s.store.ListEvents(ctx, limit)
}

func (s *Service) Storage(ctx context.Context, target string) (domain.StorageStatus, error) {
	if strings.TrimSpace(target) == "" {
		if s.cfg.SSHHost != "" {
			target = remoteSharedRoot(s.cfg.SSHHost)
		} else {
			target = "."
		}
	}
	return s.slurm.Storage(ctx, target)
}

func (s *Service) Topology(ctx context.Context, req domain.TopologyRequest) (domain.TopologyProbe, error) {
	if req.JobID != "" {
		job, err := s.GetJob(ctx, req.JobID)
		if err != nil {
			return domain.TopologyProbe{}, err
		}
		if job.SlurmJobID == "" {
			return domain.TopologyProbe{}, fmt.Errorf("job %s has no Slurm allocation to probe", job.ID)
		}
		req.SlurmJobID = job.SlurmJobID
	}
	return s.slurm.ProbeTopology(ctx, req)
}

func (s *Service) Shard(ctx context.Context, req domain.ShardRequest) (domain.ShardPlan, error) {
	nodes, err := s.store.ListNodes(ctx)
	if err != nil {
		return domain.ShardPlan{}, err
	}
	devices, err := s.store.ListDevices(ctx)
	if err != nil {
		return domain.ShardPlan{}, err
	}
	return shardplan.Recommend(req, nodes, devices)
}

func (s *Service) SubmitJob(ctx context.Context, spec domain.JobSpec) (domain.Job, error) {
	spec.Normalize()
	if spec.ID == "" {
		spec.ID = strings.ToLower(fmt.Sprintf("%s-%d", spec.Name, time.Now().Unix()))
	}
	if spec.ArtifactsDir == "" {
		if s.cfg.SSHHost != "" {
			spec.ArtifactsDir = path.Join(remoteSharedRoot(s.cfg.SSHHost), ".fuse", "artifacts", spec.ID)
		} else {
			spec.ArtifactsDir = filepath.Join(s.cfg.ArtifactsDir, spec.ID)
		}
	}
	if spec.Workdir == "" {
		if s.cfg.SSHHost != "" {
			spec.Workdir = remoteSharedRoot(s.cfg.SSHHost)
		} else {
			spec.Workdir = "."
		}
	}
	if s.cfg.SSHHost != "" {
		spec.ArtifactsDir = normalizeRemotePath(spec.Workdir, spec.ArtifactsDir)
		spec.CheckpointDir = normalizeRemotePath(spec.Workdir, spec.CheckpointDir)
	}
	if err := spec.Validate(); err != nil {
		return domain.Job{}, err
	}
	renderDir := filepath.Join(s.cfg.ArtifactsDir, spec.ID)
	job := domain.JobFromSpec(spec)
	teams, err := s.store.ListTeams(ctx)
	if err != nil {
		return domain.Job{}, err
	}
	nodes, err := s.store.ListNodes(ctx)
	if err != nil {
		return domain.Job{}, err
	}
	devices, err := s.store.ListDevices(ctx)
	if err != nil {
		return domain.Job{}, err
	}
	activeJobs, err := s.store.ListActiveJobs(ctx)
	if err != nil {
		return domain.Job{}, err
	}
	allocations, err := s.store.ListAllocations(ctx)
	if err != nil {
		return domain.Job{}, err
	}
	plan, err := s.planner.Plan(ctx, planner.Input{
		Job:         spec,
		Teams:       teams,
		Nodes:       nodes,
		Devices:     devices,
		ActiveJobs:  activeJobs,
		Allocations: allocations,
	})
	if err != nil {
		return domain.Job{}, err
	}
	if strings.TrimSpace(plan.Allocation.JobID) == "" {
		summary := plan.Why.Summary
		if strings.TrimSpace(summary) == "" {
			summary = "planner returned no allocation"
		}
		detail := plan.Why.Detail
		if strings.TrimSpace(detail) == "" {
			detail = "planner did not produce a concrete placement"
		}
		return domain.Job{}, fmt.Errorf("%s: %s", summary, detail)
	}
	job.ReasonCode = plan.Why.ReasonCode
	job.ReasonSummary = plan.Why.Summary
	job.ReasonDetail = plan.Why.Detail
	job.Suggestions = plan.Why.Suggestions
	if err := os.MkdirAll(renderDir, 0o755); err != nil {
		return domain.Job{}, err
	}
	if err := s.store.CreateJob(ctx, job, plan.Allocation); err != nil {
		return domain.Job{}, err
	}
	createdEvent, err := s.store.AddEvent(ctx, domain.Event{
		ResourceType: "job",
		ResourceID:   job.ID,
		ReasonCode:   job.ReasonCode,
		Summary:      fmt.Sprintf("job %s accepted", job.Name),
		Payload: map[string]any{
			"type":         job.Type,
			"team":         job.Team,
			"topology":     job.TopologyHint,
			"plannerWhy":   job.ReasonDetail,
			"artifactsDir": job.ArtifactsDir,
		},
	})
	if err == nil {
		s.events.Publish(createdEvent)
	}
	submitResult, err := s.slurm.Submit(ctx, spec, renderDir)
	if err != nil {
		why := domain.Why{
			ReasonCode:  domain.ReasonSubmissionFailed,
			Summary:     "slurm submission failed",
			Detail:      err.Error(),
			Suggestions: []string{"inspect sbatch output", "verify slurm commands are available"},
		}
		if _, updateErr := s.store.UpdateJobState(ctx, job.ID, domain.JobStateFailed, "SUBMISSION_FAILED", why); updateErr != nil {
			return domain.Job{}, errors.Join(err, updateErr)
		}
		job.State = domain.JobStateFailed
		job.RawState = "SUBMISSION_FAILED"
		job.ReasonCode = why.ReasonCode
		job.ReasonSummary = why.Summary
		job.ReasonDetail = why.Detail
		job.Suggestions = why.Suggestions
		return job, nil
	}
	now := time.Now().UTC()
	attempt := domain.JobAttempt{
		JobID:      job.ID,
		Attempt:    1,
		Executor:   "slurm",
		SlurmJobID: submitResult.SlurmJobID,
		RawState:   "PENDING",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.store.CreateJobAttempt(ctx, attempt); err != nil {
		return domain.Job{}, err
	}
	why := domain.Why{
		ReasonCode:  domain.ReasonScheduled,
		Summary:     "job submitted to Slurm",
		Detail:      fmt.Sprintf("planner selected %d devices and Slurm job %s accepted the request", len(plan.Allocation.DeviceIDs), submitResult.SlurmJobID),
		Suggestions: []string{"use fuse why to inspect placement", "use fuse jobs to watch runtime state"},
	}
	if _, err := s.store.UpdateJobState(ctx, job.ID, domain.JobStatePending, "PENDING", why); err != nil {
		return domain.Job{}, err
	}
	job.State = domain.JobStatePending
	job.RawState = "PENDING"
	job.ReasonCode = why.ReasonCode
	job.ReasonSummary = why.Summary
	job.ReasonDetail = why.Detail
	job.Suggestions = why.Suggestions
	job.Attempt = attempt.Attempt
	job.Executor = attempt.Executor
	job.SlurmJobID = attempt.SlurmJobID
	event, err := s.store.AddEvent(ctx, domain.Event{
		ResourceType: "job",
		ResourceID:   job.ID,
		ReasonCode:   domain.ReasonScheduled,
		Summary:      fmt.Sprintf("submitted job %s to Slurm as %s", job.Name, submitResult.SlurmJobID),
		Payload: map[string]any{
			"slurm_job_id": submitResult.SlurmJobID,
			"script_path":  submitResult.ScriptPath,
		},
	})
	if err == nil {
		s.events.Publish(event)
	}
	return job, nil
}

func (s *Service) BuildFinetuneSpec(input recipes.FinetuneInput) domain.JobSpec {
	spec := recipes.BuildFinetuneSpec(input)
	spec.ArtifactsDir = filepath.Join(s.cfg.ArtifactsDir, spec.Name)
	return spec
}

func (s *Service) CancelJob(ctx context.Context, jobID string) error {
	job, err := s.store.GetJob(ctx, jobID)
	if err != nil {
		return err
	}
	attempt, err := s.store.LatestAttemptByJob(ctx, jobID)
	if err != nil {
		return err
	}
	if err := s.slurm.Cancel(ctx, attempt.SlurmJobID); err != nil {
		return err
	}
	why := domain.Why{
		ReasonCode:  domain.ReasonCancelledByOperator,
		Summary:     "job cancellation requested",
		Detail:      fmt.Sprintf("sent scancel for Slurm job %s", attempt.SlurmJobID),
		Suggestions: []string{"wait for reconciliation to confirm terminal state"},
	}
	if _, err := s.store.UpdateJobState(ctx, jobID, domain.JobStateCancelling, "CANCELLING", why); err != nil {
		return err
	}
	event, err := s.store.AddEvent(ctx, domain.Event{
		ResourceType: "job",
		ResourceID:   jobID,
		ReasonCode:   domain.ReasonCancelledByOperator,
		Summary:      fmt.Sprintf("requested cancellation for job %s", job.Name),
		Payload: map[string]any{
			"slurm_job_id": attempt.SlurmJobID,
		},
	})
	if err == nil {
		s.events.Publish(event)
	}
	return nil
}

func (s *Service) CheckpointJob(ctx context.Context, jobID string) error {
	job, err := s.store.GetJob(ctx, jobID)
	if err != nil {
		return err
	}
	stepLabel := fmt.Sprintf("step-%d", time.Now().Unix())
	cpDir := job.CheckpointDir
	if cpDir == "" {
		cpDir = "/checkpoints/" + job.ID
	}
	cp := domain.Checkpoint{
		JobID:        jobID,
		Path:         cpDir + "/" + stepLabel,
		ProducerType: "operator",
		StepLabel:    stepLabel,
		Verified:     true,
	}
	if err := s.store.CreateCheckpoint(ctx, cp); err != nil {
		return err
	}
	event, err := s.store.AddEvent(ctx, domain.Event{
		ResourceType: "job",
		ResourceID:   jobID,
		ReasonCode:   domain.ReasonCheckpointRequested,
		Summary:      fmt.Sprintf("checkpoint requested for job %s", job.Name),
		Payload: map[string]any{
			"checkpoint_dir": cpDir,
			"step_label":     stepLabel,
		},
	})
	if err == nil {
		s.events.Publish(event)
	}
	return nil
}

func (s *Service) Checkpoints(ctx context.Context, jobID string) ([]domain.Checkpoint, error) {
	return s.store.ListCheckpointsByJob(ctx, jobID)
}

func (s *Service) Why(ctx context.Context, jobID string) (domain.Why, error) {
	job, err := s.GetJob(ctx, jobID)
	if err != nil {
		return domain.Why{}, err
	}
	why := planner.ExplainPending(job)
	why.JobID = job.ID
	return why, nil
}

func (s *Service) Simulate(ctx context.Context, req domain.SimulationRequest) (domain.SimulationResult, error) {
	teams, err := s.store.ListTeams(ctx)
	if err != nil {
		return domain.SimulationResult{}, err
	}
	nodes, err := s.store.ListNodes(ctx)
	if err != nil {
		return domain.SimulationResult{}, err
	}
	devices, err := s.store.ListDevices(ctx)
	if err != nil {
		return domain.SimulationResult{}, err
	}
	jobs, err := s.store.ListJobs(ctx)
	if err != nil {
		return domain.SimulationResult{}, err
	}
	allocations, err := s.store.ListAllocations(ctx)
	if err != nil {
		return domain.SimulationResult{}, err
	}
	result, err := s.simulator.Run(ctx, sim.Snapshot{
		Teams:       teams,
		Nodes:       nodes,
		Devices:     devices,
		Jobs:        jobs,
		Allocations: allocations,
	}, req)
	if err != nil {
		return result, err
	}
	if err := s.store.SaveSimulationRun(ctx, result); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Service) Reconcile(ctx context.Context) error {
	attempts, err := s.store.ListActiveAttempts(ctx)
	if err != nil {
		return err
	}
	for _, attempt := range attempts {
		job, err := s.store.GetJob(ctx, attempt.JobID)
		if err != nil {
			return err
		}
		status, err := s.slurm.QueryStatus(ctx, attempt.SlurmJobID)
		if err != nil {
			s.logger.Debug("slurm status unavailable", "job_id", attempt.JobID, "err", err)
			continue
		}
		nextState, reason := reconcileOutcome(job, status)
		if err := s.store.UpdateAttemptRuntime(ctx, attempt.ID, status.State, status.NodeList, status.ExitCode, status.StartedAt, status.FinishedAt); err != nil {
			return err
		}
		if nextState == domain.JobStateRunning {
			hours := s.cfg.PollInterval.Hours() * float64(job.GPUs)
			if hours > 0 {
				if err := s.store.AddGPUHours(ctx, job.Team, hours); err != nil {
					s.logger.Warn("failed to accumulate gpu hours", "job_id", job.ID, "err", err)
				}
			}
		}
		if !jobNeedsStateUpdate(job, nextState, reason) {
			continue
		}
		changed, err := s.store.UpdateJobState(ctx, attempt.JobID, nextState, status.State, reason)
		if err != nil {
			return err
		}
		if !changed {
			continue
		}
		event, err := s.store.AddEvent(ctx, domain.Event{
			ResourceType: "job",
			ResourceID:   attempt.JobID,
			ReasonCode:   reason.ReasonCode,
			Summary:      reason.Summary,
			Payload: map[string]any{
				"raw_state":    status.State,
				"slurm_job_id": status.SlurmJobID,
				"node_list":    status.NodeList,
			},
		})
		if err == nil {
			s.events.Publish(event)
		}
	}
	return nil
}

func reconcileOutcome(job domain.Job, status slurm.JobStatus) (domain.JobState, domain.Why) {
	nextState, reason := normalizeStatus(status)
	if job.State != domain.JobStateCancelling {
		return nextState, reason
	}
	switch nextState {
	case domain.JobStatePending, domain.JobStateRunning, domain.JobStateCompleting:
		return domain.JobStateCancelling, domain.Why{
			ReasonCode:  domain.ReasonCancelledByOperator,
			Summary:     "job cancellation is in progress",
			Detail:      fmt.Sprintf("waiting for Slurm to finish cancelling job %s", status.SlurmJobID),
			Suggestions: []string{"wait for Slurm to report CANCELLED or FAILED"},
			RawState:    strings.ToUpper(status.State),
		}
	default:
		return nextState, reason
	}
}

func jobNeedsStateUpdate(job domain.Job, nextState domain.JobState, reason domain.Why) bool {
	if job.State != nextState {
		return true
	}
	if job.RawState != reason.RawState {
		return true
	}
	if job.ReasonCode != reason.ReasonCode {
		return true
	}
	if job.ReasonSummary != reason.Summary {
		return true
	}
	if job.ReasonDetail != reason.Detail {
		return true
	}
	return !equalStringSlices(job.Suggestions, reason.Suggestions)
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func (s *Service) BenchmarkLocalGPU(ctx context.Context) ([]domain.Benchmark, error) {
	nvml := discovery.NewNVML()
	snapshot, err := nvml.Discover(ctx)
	if err != nil {
		return nil, err
	}
	var benchmarks []domain.Benchmark
	for _, device := range snapshot.Devices {
		benchmarks = append(benchmarks, device.Benchmark)
		if err := s.store.UpsertBenchmark(ctx, device.ID, device.Benchmark); err != nil {
			return nil, err
		}
	}
	return benchmarks, nil
}

func allocatedDeviceCount(allocations []domain.Allocation, jobs []domain.Job) int {
	active := map[string]struct{}{}
	for _, job := range jobs {
		if !job.State.Terminal() {
			active[job.ID] = struct{}{}
		}
	}
	count := 0
	for _, allocation := range allocations {
		if _, ok := active[allocation.JobID]; !ok {
			continue
		}
		count += len(allocation.DeviceIDs)
	}
	return count
}

func observedAllocatedDeviceCount(nodes []domain.Node) (int, bool) {
	total := 0
	ok := false
	for _, node := range nodes {
		if node.TotalGPUs <= 0 {
			continue
		}
		total += node.AllocatedGPUs
		ok = true
	}
	return total, ok
}

func observedDeviceCount(nodes []domain.Node) int {
	total := 0
	for _, node := range nodes {
		total += node.TotalGPUs
	}
	return total
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func normalizeStatus(status slurm.JobStatus) (domain.JobState, domain.Why) {
	raw := strings.ToUpper(status.State)
	switch raw {
	case "PENDING", "CONFIGURING":
		return domain.JobStatePending, domain.Why{
			ReasonCode: domain.ReasonSlurmQueueBacklog,
			Summary:    "job is pending in Slurm",
			Detail:     "Slurm has accepted the job but it is not running yet",
			RawState:   raw,
		}
	case "RUNNING":
		return domain.JobStateRunning, domain.Why{
			ReasonCode: domain.ReasonScheduled,
			Summary:    "job is running",
			Detail:     "Slurm started the job successfully",
			RawState:   raw,
		}
	case "COMPLETING":
		return domain.JobStateCompleting, domain.Why{
			ReasonCode: domain.ReasonScheduled,
			Summary:    "job is completing",
			Detail:     "Slurm is finalizing the job",
			RawState:   raw,
		}
	case "COMPLETED":
		return domain.JobStateSucceeded, domain.Why{
			ReasonCode: domain.ReasonScheduled,
			Summary:    "job completed successfully",
			Detail:     "Slurm marked the job as completed",
			RawState:   raw,
		}
	case "CANCELLED":
		return domain.JobStateCancelled, domain.Why{
			ReasonCode: domain.ReasonExternalCancellation,
			Summary:    "job was cancelled",
			Detail:     fmt.Sprintf("Slurm marked the job as cancelled (%s)", raw),
			RawState:   raw,
		}
	default:
		if strings.HasPrefix(raw, "CANCELLED") {
			return domain.JobStateCancelled, domain.Why{
				ReasonCode: domain.ReasonExternalCancellation,
				Summary:    "job was cancelled",
				Detail:     fmt.Sprintf("Slurm marked the job as cancelled (%s)", raw),
				RawState:   raw,
			}
		}
		return domain.JobStateFailed, domain.Why{
			ReasonCode: domain.ReasonUnknown,
			Summary:    "job failed",
			Detail:     fmt.Sprintf("Slurm returned terminal state %s", raw),
			RawState:   raw,
		}
	}
}

func (s *Service) hydrateJobs(ctx context.Context, jobs []domain.Job) ([]domain.Job, error) {
	for i := range jobs {
		if err := s.hydrateJobRuntime(ctx, &jobs[i]); err != nil {
			return nil, err
		}
	}
	return jobs, nil
}

func (s *Service) hydrateJobRuntime(ctx context.Context, job *domain.Job) error {
	attempt, err := s.store.LatestAttemptByJob(ctx, job.ID)
	if err != nil {
		if IsNotFound(err) {
			return nil
		}
		return err
	}
	applyAttemptRuntime(job, attempt)
	return nil
}

func applyAttemptRuntime(job *domain.Job, attempt domain.JobAttempt) {
	job.Attempt = attempt.Attempt
	job.Executor = attempt.Executor
	job.SlurmJobID = attempt.SlurmJobID
	job.NodeList = append([]string(nil), attempt.NodeList...)
	if attempt.ExitCode == nil || !job.State.Terminal() {
		job.ExitCode = nil
		return
	}
	code := *attempt.ExitCode
	job.ExitCode = &code
}

func IsNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

func remoteSharedRoot(sshHost string) string {
	user := "user"
	if at := strings.Index(sshHost, "@"); at > 0 {
		user = sshHost[:at]
	}
	return path.Join("/mnt/sharefs", user)
}

func normalizeRemotePath(base, value string) string {
	if value == "" {
		return value
	}
	if strings.HasPrefix(value, "/") {
		return value
	}
	return path.Join(base, value)
}
