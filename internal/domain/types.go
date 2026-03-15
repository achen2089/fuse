package domain

import "time"

type JobType string

const (
	JobTypeRun      JobType = "run"
	JobTypeTrain    JobType = "train"
	JobTypeFinetune JobType = "finetune"
)

type JobState string

const (
	JobStateDraft      JobState = "DRAFT"
	JobStateSubmitting JobState = "SUBMITTING"
	JobStatePending    JobState = "PENDING"
	JobStateRunning    JobState = "RUNNING"
	JobStateCompleting JobState = "COMPLETING"
	JobStateSucceeded  JobState = "SUCCEEDED"
	JobStateFailed     JobState = "FAILED"
	JobStateCancelling JobState = "CANCELLING"
	JobStateCancelled  JobState = "CANCELLED"
	JobStateRequeued   JobState = "REQUEUED"
)

func (s JobState) Terminal() bool {
	switch s {
	case JobStateSucceeded, JobStateFailed, JobStateCancelled:
		return true
	default:
		return false
	}
}

func (s JobState) CanTransitionTo(next JobState) bool {
	if s == next {
		return true
	}
	switch s {
	case JobStateDraft:
		return next == JobStateSubmitting
	case JobStateSubmitting:
		return next == JobStatePending || next == JobStateRunning || next == JobStateCompleting || next == JobStateSucceeded || next == JobStateCancelled || next == JobStateFailed || next == JobStateRequeued
	case JobStatePending:
		return next == JobStateRunning || next == JobStateCompleting || next == JobStateCancelling || next == JobStateSucceeded || next == JobStateCancelled || next == JobStateFailed || next == JobStateRequeued
	case JobStateRunning:
		return next == JobStateCompleting || next == JobStateCancelling || next == JobStateSucceeded || next == JobStateCancelled || next == JobStateFailed
	case JobStateCompleting:
		return next == JobStateSucceeded || next == JobStateFailed || next == JobStateCancelled
	case JobStateCancelling:
		return next == JobStateCancelled || next == JobStateFailed
	case JobStateRequeued:
		return next == JobStateSubmitting || next == JobStatePending || next == JobStateRunning || next == JobStateFailed
	default:
		return false
	}
}

const (
	PriorityLow    = "low"
	PriorityNormal = "normal"
	PriorityHigh   = "high"
)

type ReasonCode string

const (
	ReasonScheduled            ReasonCode = "scheduled"
	ReasonInsufficientGPUs     ReasonCode = "insufficient_gpus"
	ReasonTopologyUnsatisfied  ReasonCode = "topology_unsatisfied"
	ReasonQuotaExceeded        ReasonCode = "quota_exceeded"
	ReasonSlurmQueueBacklog    ReasonCode = "slurm_queue_backpressure"
	ReasonSubmissionFailed     ReasonCode = "submission_failed"
	ReasonUnknown              ReasonCode = "unknown"
	ReasonCheckpointRequested  ReasonCode = "checkpoint_requested"
	ReasonCancelledByOperator  ReasonCode = "cancelled_by_operator"
	ReasonExternalCancellation ReasonCode = "external_cancellation"
)

type TopologyHint string

const (
	TopologyAny        TopologyHint = "any"
	TopologySameNode   TopologyHint = "same_node"
	TopologySameSwitch TopologyHint = "same_switch"
)

type CheckpointMode string

const (
	CheckpointNone       CheckpointMode = "none"
	CheckpointFilesystem CheckpointMode = "filesystem"
)

type DiscoverySource string

const (
	DiscoverySourceFaker DiscoverySource = "faker"
	DiscoverySourceNVML  DiscoverySource = "nvml"
	DiscoverySourceSlurm DiscoverySource = "slurm"
)

type HealthStatus string

const (
	HealthHealthy  HealthStatus = "healthy"
	HealthDegraded HealthStatus = "degraded"
	HealthOffline  HealthStatus = "offline"
)

type Team struct {
	Name         string    `json:"name"`
	QuotaGPUs    int       `json:"quota_gpus"`
	BurstEnabled bool      `json:"burst_enabled"`
	GPUHours     float64   `json:"gpu_hours"`
	CreatedAt    time.Time `json:"created_at"`
}

type Node struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	SwitchName      string          `json:"switch_name"`
	Rack            string          `json:"rack"`
	Health          HealthStatus    `json:"health"`
	DiscoverySource DiscoverySource `json:"discovery_source"`
	TotalGPUs       int             `json:"total_gpus"`
	AllocatedGPUs   int             `json:"allocated_gpus"`
	FreeGPUs        int             `json:"free_gpus"`
	ObservedState   string          `json:"observed_state,omitempty"`
	Real            bool            `json:"real"`
}

type Device struct {
	ID        string       `json:"id"`
	NodeID    string       `json:"node_id"`
	GPUIndex  int          `json:"gpu_index"`
	Vendor    string       `json:"vendor"`
	Model     string       `json:"model"`
	MemoryMB  int64        `json:"memory_mb"`
	Health    HealthStatus `json:"health"`
	Real      bool         `json:"real"`
	Benchmark Benchmark    `json:"benchmark"`
}

type Benchmark struct {
	GPUName        string    `json:"gpu_name"`
	MemoryMB       int64     `json:"memory_mb"`
	TemperatureC   int       `json:"temperature_c"`
	UtilizationPct int       `json:"utilization_pct"`
	MeasuredAt     time.Time `json:"measured_at"`
}

type FabricLink struct {
	SourceNodeID  string `json:"source_node_id"`
	TargetNodeID  string `json:"target_node_id"`
	Tier          string `json:"tier"`
	BandwidthGbps int    `json:"bandwidth_gbps"`
	LatencyClass  string `json:"latency_class"`
	Bidirectional bool   `json:"bidirectional"`
}

type JobSpec struct {
	ID                 string            `json:"id"`
	Name               string            `json:"name"`
	Team               string            `json:"team"`
	Type               JobType           `json:"type"`
	CommandOrRecipe    string            `json:"command_or_recipe"`
	Workdir            string            `json:"workdir"`
	ContainerImage     string            `json:"container_image,omitempty"`
	ContainerMounts    []string          `json:"container_mounts,omitempty"`
	ContainerWorkdir   string            `json:"container_workdir,omitempty"`
	ContainerMountHome bool              `json:"container_mount_home,omitempty"`
	Env                map[string]string `json:"env"`
	GPUs               int               `json:"gpus"`
	Nodes              int               `json:"nodes,omitempty"`
	Tasks              int               `json:"tasks,omitempty"`
	TasksPerNode       int               `json:"tasks_per_node,omitempty"`
	GPUsPerNode        int               `json:"gpus_per_node,omitempty"`
	CPUs               int               `json:"cpus"`
	MemoryMB           int64             `json:"memory_mb"`
	Walltime           string            `json:"walltime"`
	CheckpointMode     CheckpointMode    `json:"checkpoint_mode"`
	CheckpointDir      string            `json:"checkpoint_dir"`
	ResumeCommand      string            `json:"resume_command"`
	Preemptable        bool              `json:"preemptable"`
	PriorityHint       string            `json:"priority_hint"`
	TopologyHint       TopologyHint      `json:"topology_hint"`
	ArtifactsDir       string            `json:"artifacts_dir"`
}

type Job struct {
	ID                 string            `json:"id"`
	Name               string            `json:"name"`
	Team               string            `json:"team"`
	Type               JobType           `json:"type"`
	CommandOrRecipe    string            `json:"command_or_recipe"`
	Workdir            string            `json:"workdir"`
	ContainerImage     string            `json:"container_image,omitempty"`
	ContainerMounts    []string          `json:"container_mounts,omitempty"`
	ContainerWorkdir   string            `json:"container_workdir,omitempty"`
	ContainerMountHome bool              `json:"container_mount_home,omitempty"`
	Env                map[string]string `json:"env"`
	GPUs               int               `json:"gpus"`
	CPUs               int               `json:"cpus"`
	MemoryMB           int64             `json:"memory_mb"`
	Walltime           string            `json:"walltime"`
	CheckpointMode     CheckpointMode    `json:"checkpoint_mode"`
	CheckpointDir      string            `json:"checkpoint_dir"`
	ResumeCommand      string            `json:"resume_command"`
	Preemptable        bool              `json:"preemptable"`
	PriorityHint       string            `json:"priority_hint"`
	TopologyHint       TopologyHint      `json:"topology_hint"`
	ArtifactsDir       string            `json:"artifacts_dir"`
	DesiredState       JobState          `json:"desired_state"`
	State              JobState          `json:"state"`
	RawState           string            `json:"raw_state"`
	ReasonCode         ReasonCode        `json:"reason_code"`
	ReasonSummary      string            `json:"reason_summary"`
	ReasonDetail       string            `json:"reason_detail"`
	Suggestions        []string          `json:"suggestions"`
	Attempt            int               `json:"attempt"`
	Executor           string            `json:"executor,omitempty"`
	SlurmJobID         string            `json:"slurm_job_id,omitempty"`
	NodeList           []string          `json:"node_list,omitempty"`
	ExitCode           *int              `json:"exit_code,omitempty"`
	CreatedAt          time.Time         `json:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
}

func JobFromSpec(spec JobSpec) Job {
	now := time.Now().UTC()
	return Job{
		ID:                 spec.ID,
		Name:               spec.Name,
		Team:               spec.Team,
		Type:               spec.Type,
		CommandOrRecipe:    spec.CommandOrRecipe,
		Workdir:            spec.Workdir,
		ContainerImage:     spec.ContainerImage,
		ContainerMounts:    cloneStringSlice(spec.ContainerMounts),
		ContainerWorkdir:   spec.ContainerWorkdir,
		ContainerMountHome: spec.ContainerMountHome,
		Env:                cloneStringMap(spec.Env),
		GPUs:               spec.GPUs,
		CPUs:               spec.CPUs,
		MemoryMB:           spec.MemoryMB,
		Walltime:           spec.Walltime,
		CheckpointMode:     spec.CheckpointMode,
		CheckpointDir:      spec.CheckpointDir,
		ResumeCommand:      spec.ResumeCommand,
		Preemptable:        spec.Preemptable,
		PriorityHint:       spec.PriorityHint,
		TopologyHint:       spec.TopologyHint,
		ArtifactsDir:       spec.ArtifactsDir,
		DesiredState:       JobStatePending,
		State:              JobStateSubmitting,
		ReasonCode:         ReasonUnknown,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

type JobAttempt struct {
	ID         int64     `json:"id"`
	JobID      string    `json:"job_id"`
	Attempt    int       `json:"attempt"`
	Executor   string    `json:"executor"`
	SlurmJobID string    `json:"slurm_job_id"`
	RawState   string    `json:"raw_state"`
	ExitCode   *int      `json:"exit_code"`
	NodeList   []string  `json:"node_list"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Allocation struct {
	ID           int64     `json:"id"`
	JobID        string    `json:"job_id"`
	NodeIDs      []string  `json:"node_ids"`
	DeviceIDs    []string  `json:"device_ids"`
	PlannerScore int       `json:"planner_score"`
	Constraints  []string  `json:"constraints"`
	CreatedAt    time.Time `json:"created_at"`
}

type Checkpoint struct {
	ID           int64     `json:"id"`
	JobID        string    `json:"job_id"`
	Path         string    `json:"path"`
	ProducerType string    `json:"producer_type"`
	StepLabel    string    `json:"step_label"`
	Verified     bool      `json:"verified"`
	CreatedAt    time.Time `json:"created_at"`
}

type Event struct {
	ID           int64          `json:"id"`
	CreatedAt    time.Time      `json:"created_at"`
	ResourceType string         `json:"resource_type"`
	ResourceID   string         `json:"resource_id"`
	ReasonCode   ReasonCode     `json:"reason_code"`
	Summary      string         `json:"summary"`
	Payload      map[string]any `json:"payload"`
}

type Why struct {
	JobID        string     `json:"job_id"`
	ReasonCode   ReasonCode `json:"reason_code"`
	Summary      string     `json:"summary"`
	Detail       string     `json:"detail"`
	Suggestions  []string   `json:"suggestions"`
	SlurmJobID   string     `json:"slurm_job_id,omitempty"`
	NodeList     []string   `json:"node_list,omitempty"`
	ExitCode     *int       `json:"exit_code,omitempty"`
	RawState     string     `json:"raw_state"`
	CurrentState JobState   `json:"current_state"`
}

type JobLog struct {
	JobID      string `json:"job_id"`
	JobName    string `json:"job_name"`
	SlurmJobID string `json:"slurm_job_id"`
	Stream     string `json:"stream"`
	Path       string `json:"path"`
	TailLines  int    `json:"tail_lines"`
	Content    string `json:"content"`
}

type StorageFilesystem struct {
	Source         string `json:"source"`
	Target         string `json:"target"`
	SizeBytes      int64  `json:"size_bytes"`
	UsedBytes      int64  `json:"used_bytes"`
	AvailableBytes int64  `json:"available_bytes"`
	UsePercent     int    `json:"use_percent"`
}

type StorageStatus struct {
	Path        string              `json:"path"`
	Filesystems []StorageFilesystem `json:"filesystems"`
	CheckedAt   time.Time           `json:"checked_at"`
	Raw         string              `json:"raw,omitempty"`
}

type TopologyRequest struct {
	JobID            string `json:"job_id,omitempty"`
	SlurmJobID       string `json:"slurm_job_id,omitempty"`
	Node             string `json:"node,omitempty"`
	GPUs             int    `json:"gpus,omitempty"`
	CPUs             int    `json:"cpus,omitempty"`
	MemoryMB         int64  `json:"memory_mb,omitempty"`
	Walltime         string `json:"walltime,omitempty"`
	ImmediateSeconds int    `json:"immediate_seconds,omitempty"`
}

type TopologyNode struct {
	Node   string `json:"node"`
	Matrix string `json:"matrix"`
}

type TopologyProbe struct {
	Mode          string         `json:"mode"`
	JobID         string         `json:"job_id,omitempty"`
	SlurmJobID    string         `json:"slurm_job_id,omitempty"`
	RequestedGPUs int            `json:"requested_gpus,omitempty"`
	Nodes         []TopologyNode `json:"nodes"`
	CheckedAt     time.Time      `json:"checked_at"`
}

type SimulationAction string

const (
	SimulationKillNode SimulationAction = "kill_node"
	SimulationAddNode  SimulationAction = "add_node"
	SimulationSubmit   SimulationAction = "submit_job"
)

type SimulationRequest struct {
	Action     SimulationAction `json:"action"`
	NodeID     string           `json:"node_id,omitempty"`
	AddNodes   int              `json:"add_nodes,omitempty"`
	SwitchName string           `json:"switch_name,omitempty"`
	SubmitSpec *JobSpec         `json:"submit_spec,omitempty"`
}

type SimulationResult struct {
	ID            string           `json:"id"`
	Action        SimulationAction `json:"action"`
	Summary       string           `json:"summary"`
	AffectedJobs  []string         `json:"affected_jobs"`
	RecoveredJobs []string         `json:"recovered_jobs"`
	FailedJobs    []string         `json:"failed_jobs"`
	CreatedAt     time.Time        `json:"created_at"`
}

type ClusterStatus struct {
	Nodes       int `json:"nodes"`
	Devices     int `json:"devices"`
	Allocated   int `json:"allocated"`
	Idle        int `json:"idle"`
	RunningJobs int `json:"running_jobs"`
	PendingJobs int `json:"pending_jobs"`
	FailedJobs  int `json:"failed_jobs"`
}

type ShardRequest struct {
	Model  string `json:"model"`
	GPUs   int    `json:"gpus"`
	Nodes  int    `json:"nodes,omitempty"`
	Method string `json:"method,omitempty"`
}

type ShardPlan struct {
	Model                   string       `json:"model"`
	Method                  string       `json:"method"`
	GPUs                    int          `json:"gpus"`
	Nodes                   int          `json:"nodes"`
	GPUsPerNode             int          `json:"gpus_per_node"`
	DeviceMemoryGB          float64      `json:"device_memory_gb"`
	WeightGB                float64      `json:"weight_gb"`
	TensorParallel          int          `json:"tensor_parallel"`
	PipelineParallel        int          `json:"pipeline_parallel"`
	DataParallel            int          `json:"data_parallel"`
	PerGPUWeightGB          float64      `json:"per_gpu_weight_gb"`
	EstimatedPerGPUMemoryGB float64      `json:"estimated_per_gpu_memory_gb"`
	Fits                    bool         `json:"fits"`
	TopologyHint            TopologyHint `json:"topology_hint"`
	Summary                 string       `json:"summary"`
	Detail                  string       `json:"detail"`
	Suggestions             []string     `json:"suggestions,omitempty"`
}
