package main

import (
	"context"

	"fuse/internal/domain"
	"fuse/internal/server"
)

type directCLI struct {
	svc *server.Service
}

func newDirectCLI(ctx context.Context, cfg server.Config) (*directCLI, error) {
	svc, err := server.New(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &directCLI{svc: svc}, nil
}

func (d *directCLI) Close() error {
	return d.svc.Close()
}

func (d *directCLI) Status(ctx context.Context) (domain.ClusterStatus, error) {
	_ = d.svc.Reconcile(ctx)
	return d.svc.Status(ctx)
}

func (d *directCLI) Nodes(ctx context.Context) ([]domain.Node, []domain.Device, error) {
	_ = d.svc.Reconcile(ctx)
	return d.svc.Nodes(ctx)
}

func (d *directCLI) Fabric(ctx context.Context) ([]domain.FabricLink, error) {
	_ = d.svc.Reconcile(ctx)
	return d.svc.Fabric(ctx)
}

func (d *directCLI) Teams(ctx context.Context) ([]domain.Team, error) {
	_ = d.svc.Reconcile(ctx)
	return d.svc.Teams(ctx)
}

func (d *directCLI) Jobs(ctx context.Context) ([]domain.Job, error) {
	_ = d.svc.Reconcile(ctx)
	return d.svc.Jobs(ctx)
}

func (d *directCLI) Events(ctx context.Context, limit int) ([]domain.Event, error) {
	_ = d.svc.Reconcile(ctx)
	return d.svc.Events(ctx, limit)
}

func (d *directCLI) Storage(ctx context.Context, target string) (domain.StorageStatus, error) {
	return d.svc.Storage(ctx, target)
}

func (d *directCLI) Topology(ctx context.Context, req domain.TopologyRequest) (domain.TopologyProbe, error) {
	_ = d.svc.Reconcile(ctx)
	return d.svc.Topology(ctx, req)
}

func (d *directCLI) Shard(ctx context.Context, req domain.ShardRequest) (domain.ShardPlan, error) {
	_ = d.svc.Reconcile(ctx)
	return d.svc.Shard(ctx, req)
}

func (d *directCLI) Submit(ctx context.Context, spec domain.JobSpec) (domain.Job, error) {
	return d.svc.SubmitJob(ctx, spec)
}

func (d *directCLI) Logs(ctx context.Context, jobID, stream string, tailLines int) (domain.JobLog, error) {
	_ = d.svc.Reconcile(ctx)
	return d.svc.Logs(ctx, jobID, stream, tailLines)
}

func (d *directCLI) Why(ctx context.Context, jobID string) (domain.Why, error) {
	_ = d.svc.Reconcile(ctx)
	return d.svc.Why(ctx, jobID)
}

func (d *directCLI) Cancel(ctx context.Context, jobID string) error {
	return d.svc.CancelJob(ctx, jobID)
}

func (d *directCLI) Checkpoint(ctx context.Context, jobID string) error {
	return d.svc.CheckpointJob(ctx, jobID)
}

func (d *directCLI) Checkpoints(ctx context.Context, jobID string) ([]domain.Checkpoint, error) {
	return d.svc.Checkpoints(ctx, jobID)
}

func (d *directCLI) Simulate(ctx context.Context, req domain.SimulationRequest) (domain.SimulationResult, error) {
	_ = d.svc.Reconcile(ctx)
	return d.svc.Simulate(ctx, req)
}
