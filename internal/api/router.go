package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"fuse/internal/domain"
	"fuse/internal/server"
)

type Service interface {
	Status(ctx context.Context) (domain.ClusterStatus, error)
	Nodes(ctx context.Context) ([]domain.Node, []domain.Device, error)
	Fabric(ctx context.Context) ([]domain.FabricLink, error)
	Teams(ctx context.Context) ([]domain.Team, error)
	Jobs(ctx context.Context) ([]domain.Job, error)
	GetJob(ctx context.Context, jobID string) (domain.Job, error)
	Events(ctx context.Context, limit int) ([]domain.Event, error)
	Storage(ctx context.Context, target string) (domain.StorageStatus, error)
	Topology(ctx context.Context, req domain.TopologyRequest) (domain.TopologyProbe, error)
	Shard(ctx context.Context, req domain.ShardRequest) (domain.ShardPlan, error)
	Logs(ctx context.Context, jobID, stream string, tailLines int) (domain.JobLog, error)
	SubmitJob(ctx context.Context, spec domain.JobSpec) (domain.Job, error)
	CancelJob(ctx context.Context, jobID string) error
	CheckpointJob(ctx context.Context, jobID string) error
	Checkpoints(ctx context.Context, jobID string) ([]domain.Checkpoint, error)
	Why(ctx context.Context, jobID string) (domain.Why, error)
	Simulate(ctx context.Context, req domain.SimulationRequest) (domain.SimulationResult, error)
	SubscribeEvents() chan domain.Event
	UnsubscribeEvents(ch chan domain.Event)
}

type Router struct {
	svc Service
}

func NewRouter(svc Service) http.Handler {
	return &Router{svc: svc}
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case req.Method == http.MethodGet && req.URL.Path == "/v1/status":
		status, err := r.svc.Status(req.Context())
		r.respond(w, status, err)
	case req.Method == http.MethodGet && req.URL.Path == "/v1/nodes":
		nodes, devices, err := r.svc.Nodes(req.Context())
		r.respond(w, map[string]any{"nodes": nodes, "devices": devices}, err)
	case req.Method == http.MethodGet && req.URL.Path == "/v1/fabric":
		links, err := r.svc.Fabric(req.Context())
		if links == nil {
			links = []domain.FabricLink{}
		}
		r.respond(w, links, err)
	case req.Method == http.MethodGet && req.URL.Path == "/v1/teams":
		teams, err := r.svc.Teams(req.Context())
		if teams == nil {
			teams = []domain.Team{}
		}
		r.respond(w, teams, err)
	case req.Method == http.MethodGet && req.URL.Path == "/v1/jobs":
		jobs, err := r.svc.Jobs(req.Context())
		if jobs == nil {
			jobs = []domain.Job{}
		}
		r.respond(w, jobs, err)
	case req.Method == http.MethodGet && req.URL.Path == "/v1/checkpoints":
		jobID := req.URL.Query().Get("job_id")
		if jobID == "" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("job_id query parameter is required"))
			return
		}
		checkpoints, err := r.svc.Checkpoints(req.Context(), jobID)
		if checkpoints == nil {
			checkpoints = []domain.Checkpoint{}
		}
		r.respond(w, checkpoints, err)
	case req.Method == http.MethodGet && req.URL.Path == "/v1/events":
		limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
		events, err := r.svc.Events(req.Context(), limit)
		if events == nil {
			events = []domain.Event{}
		}
		r.respond(w, events, err)
	case req.Method == http.MethodGet && req.URL.Path == "/v1/storage":
		status, err := r.svc.Storage(req.Context(), req.URL.Query().Get("path"))
		r.respond(w, status, err)
	case req.Method == http.MethodPost && req.URL.Path == "/v1/shard":
		var shardReq domain.ShardRequest
		if err := json.NewDecoder(req.Body).Decode(&shardReq); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		plan, err := r.svc.Shard(req.Context(), shardReq)
		r.respond(w, plan, err)
	case req.Method == http.MethodGet && req.URL.Path == "/v1/events/stream":
		r.streamEvents(w, req)
	case req.Method == http.MethodPost && req.URL.Path == "/v1/jobs":
		var spec domain.JobSpec
		if err := json.NewDecoder(req.Body).Decode(&spec); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		job, err := r.svc.SubmitJob(req.Context(), spec)
		r.respond(w, job, err)
	case req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/logs"):
		jobID := strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, "/v1/jobs/"), "/logs")
		tail, _ := strconv.Atoi(req.URL.Query().Get("tail"))
		logs, err := r.svc.Logs(req.Context(), jobID, req.URL.Query().Get("stream"), tail)
		r.respond(w, logs, err)
	case req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/why"):
		jobID := strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, "/v1/jobs/"), "/why")
		why, err := r.svc.Why(req.Context(), jobID)
		r.respond(w, why, err)
	case req.Method == http.MethodGet && strings.HasPrefix(req.URL.Path, "/v1/jobs/") && !strings.Contains(strings.TrimPrefix(req.URL.Path, "/v1/jobs/"), "/"):
		jobID := strings.TrimPrefix(req.URL.Path, "/v1/jobs/")
		job, err := r.svc.GetJob(req.Context(), jobID)
		r.respond(w, job, err)
	case req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/cancel"):
		jobID := strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, "/v1/jobs/"), "/cancel")
		r.respond(w, map[string]string{"status": "ok"}, r.svc.CancelJob(req.Context(), jobID))
	case req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/checkpoint"):
		jobID := strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, "/v1/jobs/"), "/checkpoint")
		r.respond(w, map[string]string{"status": "ok"}, r.svc.CheckpointJob(req.Context(), jobID))
	case req.Method == http.MethodPost && req.URL.Path == "/v1/simulations":
		var simReq domain.SimulationRequest
		if err := json.NewDecoder(req.Body).Decode(&simReq); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		result, err := r.svc.Simulate(req.Context(), simReq)
		r.respond(w, result, err)
	case req.Method == http.MethodPost && req.URL.Path == "/v1/topology/probe":
		var topoReq domain.TopologyRequest
		if err := json.NewDecoder(req.Body).Decode(&topoReq); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		probe, err := r.svc.Topology(req.Context(), topoReq)
		r.respond(w, probe, err)
	default:
		writeError(w, http.StatusNotFound, fmt.Errorf("route not found"))
	}
}

func (r *Router) streamEvents(w http.ResponseWriter, req *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch := r.svc.SubscribeEvents()
	defer r.svc.UnsubscribeEvents(ch)
	for {
		select {
		case <-req.Context().Done():
			return
		case event := <-ch:
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

func (r *Router) respond(w http.ResponseWriter, value any, err error) {
	if err != nil {
		status := http.StatusInternalServerError
		if server.IsNotFound(err) {
			status = http.StatusNotFound
		}
		writeError(w, status, err)
		return
	}
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
