package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"fuse/internal/domain"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_foreign_keys=on&_busy_timeout=5000", filepath.ToSlash(path))
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db}
	if err := store.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	stmts := []string{
		`PRAGMA journal_mode=WAL;`,
		`CREATE TABLE IF NOT EXISTS teams (
			name TEXT PRIMARY KEY,
			quota_gpus INTEGER NOT NULL,
			burst_enabled INTEGER NOT NULL,
			gpu_hours REAL NOT NULL,
			created_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS nodes (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			switch_name TEXT NOT NULL,
			rack TEXT NOT NULL,
			health TEXT NOT NULL,
			discovery_source TEXT NOT NULL,
			total_gpus INTEGER NOT NULL DEFAULT 0,
			allocated_gpus INTEGER NOT NULL DEFAULT 0,
			free_gpus INTEGER NOT NULL DEFAULT 0,
			observed_state TEXT NOT NULL DEFAULT '',
			real INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS devices (
			id TEXT PRIMARY KEY,
			node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
			gpu_index INTEGER NOT NULL,
			vendor TEXT NOT NULL,
			model TEXT NOT NULL,
			memory_mb INTEGER NOT NULL,
			health TEXT NOT NULL,
			real INTEGER NOT NULL,
			benchmark_json TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS fabric_links (
			source_node_id TEXT NOT NULL,
			target_node_id TEXT NOT NULL,
			tier TEXT NOT NULL,
			bandwidth_gbps INTEGER NOT NULL,
			latency_class TEXT NOT NULL,
			bidirectional INTEGER NOT NULL,
			PRIMARY KEY (source_node_id, target_node_id)
		);`,
		`CREATE TABLE IF NOT EXISTS jobs (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			team TEXT NOT NULL,
			type TEXT NOT NULL,
			command_or_recipe TEXT NOT NULL,
			workdir TEXT NOT NULL,
			container_image TEXT NOT NULL DEFAULT '',
			container_mounts_json TEXT NOT NULL DEFAULT '[]',
			container_workdir TEXT NOT NULL DEFAULT '',
			container_mount_home INTEGER NOT NULL DEFAULT 0,
			env_json TEXT NOT NULL,
			gpus INTEGER NOT NULL,
			cpus INTEGER NOT NULL,
			memory_mb INTEGER NOT NULL,
			walltime TEXT NOT NULL,
			checkpoint_mode TEXT NOT NULL,
			checkpoint_dir TEXT NOT NULL,
			resume_command TEXT NOT NULL,
			preemptable INTEGER NOT NULL,
			priority_hint TEXT NOT NULL,
			topology_hint TEXT NOT NULL,
			artifacts_dir TEXT NOT NULL,
			desired_state TEXT NOT NULL,
			state TEXT NOT NULL,
			raw_state TEXT NOT NULL,
			reason_code TEXT NOT NULL,
			reason_summary TEXT NOT NULL,
			reason_detail TEXT NOT NULL,
			suggestions_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS job_attempts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
			attempt INTEGER NOT NULL,
			executor TEXT NOT NULL,
			slurm_job_id TEXT NOT NULL,
			raw_state TEXT NOT NULL,
			exit_code INTEGER,
			node_list_json TEXT NOT NULL,
			started_at TEXT NOT NULL,
			finished_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS allocations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
			node_ids_json TEXT NOT NULL,
			device_ids_json TEXT NOT NULL,
			planner_score INTEGER NOT NULL,
			constraints_json TEXT NOT NULL,
			created_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS checkpoints (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
			path TEXT NOT NULL,
			producer_type TEXT NOT NULL,
			step_label TEXT NOT NULL,
			verified INTEGER NOT NULL,
			created_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at TEXT NOT NULL,
			resource_type TEXT NOT NULL,
			resource_id TEXT NOT NULL,
			reason_code TEXT NOT NULL,
			summary TEXT NOT NULL,
			payload_json TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS benchmarks (
			device_id TEXT PRIMARY KEY,
			payload_json TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS simulation_runs (
			id TEXT PRIMARY KEY,
			action TEXT NOT NULL,
			summary TEXT NOT NULL,
			affected_jobs_json TEXT NOT NULL,
			recovered_jobs_json TEXT NOT NULL,
			failed_jobs_json TEXT NOT NULL,
			created_at TEXT NOT NULL
		);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	for _, migration := range []struct {
		table  string
		column string
		ddl    string
	}{
		{table: "nodes", column: "total_gpus", ddl: `ALTER TABLE nodes ADD COLUMN total_gpus INTEGER NOT NULL DEFAULT 0`},
		{table: "nodes", column: "allocated_gpus", ddl: `ALTER TABLE nodes ADD COLUMN allocated_gpus INTEGER NOT NULL DEFAULT 0`},
		{table: "nodes", column: "free_gpus", ddl: `ALTER TABLE nodes ADD COLUMN free_gpus INTEGER NOT NULL DEFAULT 0`},
		{table: "nodes", column: "observed_state", ddl: `ALTER TABLE nodes ADD COLUMN observed_state TEXT NOT NULL DEFAULT ''`},
		{table: "jobs", column: "container_image", ddl: `ALTER TABLE jobs ADD COLUMN container_image TEXT NOT NULL DEFAULT ''`},
		{table: "jobs", column: "container_mounts_json", ddl: `ALTER TABLE jobs ADD COLUMN container_mounts_json TEXT NOT NULL DEFAULT '[]'`},
		{table: "jobs", column: "container_workdir", ddl: `ALTER TABLE jobs ADD COLUMN container_workdir TEXT NOT NULL DEFAULT ''`},
		{table: "jobs", column: "container_mount_home", ddl: `ALTER TABLE jobs ADD COLUMN container_mount_home INTEGER NOT NULL DEFAULT 0`},
	} {
		if err := s.ensureColumn(ctx, migration.table, migration.column, migration.ddl); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ReplaceClusterSnapshot(ctx context.Context, nodes []domain.Node, devices []domain.Device, links []domain.FabricLink) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	for _, stmt := range []string{
		`DELETE FROM benchmarks;`,
		`DELETE FROM fabric_links;`,
		`DELETE FROM devices;`,
		`DELETE FROM nodes;`,
	} {
		if _, err = tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	for _, node := range nodes {
		if _, err = tx.ExecContext(ctx,
			`INSERT INTO nodes (id, name, switch_name, rack, health, discovery_source, total_gpus, allocated_gpus, free_gpus, observed_state, real)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			node.ID, node.Name, node.SwitchName, node.Rack, node.Health, node.DiscoverySource, node.TotalGPUs, node.AllocatedGPUs, node.FreeGPUs, node.ObservedState, boolToInt(node.Real),
		); err != nil {
			return err
		}
	}
	for _, device := range devices {
		benchmarkJSON, err2 := marshalJSON(device.Benchmark)
		if err2 != nil {
			return err2
		}
		if _, err = tx.ExecContext(ctx,
			`INSERT INTO devices (id, node_id, gpu_index, vendor, model, memory_mb, health, real, benchmark_json)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			device.ID, device.NodeID, device.GPUIndex, device.Vendor, device.Model, device.MemoryMB, device.Health, boolToInt(device.Real), benchmarkJSON,
		); err != nil {
			return err
		}
	}
	for _, link := range links {
		if _, err = tx.ExecContext(ctx,
			`INSERT INTO fabric_links (source_node_id, target_node_id, tier, bandwidth_gbps, latency_class, bidirectional)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			link.SourceNodeID, link.TargetNodeID, link.Tier, link.BandwidthGbps, link.LatencyClass, boolToInt(link.Bidirectional),
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) UpsertTeam(ctx context.Context, team domain.Team) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO teams (name, quota_gpus, burst_enabled, gpu_hours, created_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET quota_gpus=excluded.quota_gpus, burst_enabled=excluded.burst_enabled, gpu_hours=excluded.gpu_hours`,
		team.Name, team.QuotaGPUs, boolToInt(team.BurstEnabled), team.GPUHours, team.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (s *Store) ListTeams(ctx context.Context) ([]domain.Team, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, quota_gpus, burst_enabled, gpu_hours, created_at FROM teams ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var teams []domain.Team
	for rows.Next() {
		var team domain.Team
		var burst int
		var created string
		if err := rows.Scan(&team.Name, &team.QuotaGPUs, &burst, &team.GPUHours, &created); err != nil {
			return nil, err
		}
		team.BurstEnabled = burst == 1
		team.CreatedAt = mustTime(created)
		teams = append(teams, team)
	}
	return teams, rows.Err()
}

func (s *Store) ListNodes(ctx context.Context) ([]domain.Node, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, switch_name, rack, health, discovery_source, total_gpus, allocated_gpus, free_gpus, observed_state, real FROM nodes ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var nodes []domain.Node
	for rows.Next() {
		var node domain.Node
		var real int
		if err := rows.Scan(&node.ID, &node.Name, &node.SwitchName, &node.Rack, &node.Health, &node.DiscoverySource, &node.TotalGPUs, &node.AllocatedGPUs, &node.FreeGPUs, &node.ObservedState, &real); err != nil {
			return nil, err
		}
		node.Real = real == 1
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

func (s *Store) ListDevices(ctx context.Context) ([]domain.Device, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, node_id, gpu_index, vendor, model, memory_mb, health, real, benchmark_json FROM devices ORDER BY node_id, gpu_index`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var devices []domain.Device
	for rows.Next() {
		var device domain.Device
		var real int
		var benchmarkJSON string
		if err := rows.Scan(&device.ID, &device.NodeID, &device.GPUIndex, &device.Vendor, &device.Model, &device.MemoryMB, &device.Health, &real, &benchmarkJSON); err != nil {
			return nil, err
		}
		device.Real = real == 1
		if err := json.Unmarshal([]byte(benchmarkJSON), &device.Benchmark); err != nil {
			return nil, err
		}
		devices = append(devices, device)
	}
	return devices, rows.Err()
}

func (s *Store) ListFabricLinks(ctx context.Context) ([]domain.FabricLink, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT source_node_id, target_node_id, tier, bandwidth_gbps, latency_class, bidirectional FROM fabric_links ORDER BY source_node_id, target_node_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var links []domain.FabricLink
	for rows.Next() {
		var link domain.FabricLink
		var bidirectional int
		if err := rows.Scan(&link.SourceNodeID, &link.TargetNodeID, &link.Tier, &link.BandwidthGbps, &link.LatencyClass, &bidirectional); err != nil {
			return nil, err
		}
		link.Bidirectional = bidirectional == 1
		links = append(links, link)
	}
	return links, rows.Err()
}

func (s *Store) CreateJob(ctx context.Context, job domain.Job, allocation domain.Allocation) error {
	envJSON, err := marshalJSON(job.Env)
	if err != nil {
		return err
	}
	containerMountsJSON, err := marshalJSON(job.ContainerMounts)
	if err != nil {
		return err
	}
	suggestionsJSON, err := marshalJSON(job.Suggestions)
	if err != nil {
		return err
	}
	nodeIDs, err := marshalJSON(allocation.NodeIDs)
	if err != nil {
		return err
	}
	deviceIDs, err := marshalJSON(allocation.DeviceIDs)
	if err != nil {
		return err
	}
	constraints, err := marshalJSON(allocation.Constraints)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err = tx.ExecContext(ctx,
		`INSERT INTO jobs (
			id, name, team, type, command_or_recipe, workdir, container_image, container_mounts_json, container_workdir, container_mount_home, env_json, gpus, cpus, memory_mb, walltime,
			checkpoint_mode, checkpoint_dir, resume_command, preemptable, priority_hint, topology_hint, artifacts_dir,
			desired_state, state, raw_state, reason_code, reason_summary, reason_detail, suggestions_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.Name, job.Team, job.Type, job.CommandOrRecipe, job.Workdir, job.ContainerImage, containerMountsJSON, job.ContainerWorkdir, boolToInt(job.ContainerMountHome), envJSON, job.GPUs, job.CPUs, job.MemoryMB,
		job.Walltime, job.CheckpointMode, job.CheckpointDir, job.ResumeCommand, boolToInt(job.Preemptable), job.PriorityHint,
		job.TopologyHint, job.ArtifactsDir, job.DesiredState, job.State, job.RawState, job.ReasonCode, job.ReasonSummary,
		job.ReasonDetail, suggestionsJSON, job.CreatedAt.UTC().Format(time.RFC3339Nano), job.UpdatedAt.UTC().Format(time.RFC3339Nano),
	); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx,
		`INSERT INTO allocations (job_id, node_ids_json, device_ids_json, planner_score, constraints_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		allocation.JobID, nodeIDs, deviceIDs, allocation.PlannerScore, constraints, allocation.CreatedAt.UTC().Format(time.RFC3339Nano),
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CreateJobAttempt(ctx context.Context, attempt domain.JobAttempt) error {
	nodeListJSON, err := marshalJSON(attempt.NodeList)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO job_attempts (job_id, attempt, executor, slurm_job_id, raw_state, exit_code, node_list_json, started_at, finished_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		attempt.JobID, attempt.Attempt, attempt.Executor, attempt.SlurmJobID, attempt.RawState, attempt.ExitCode, nodeListJSON,
		timeString(attempt.StartedAt), timeString(attempt.FinishedAt), timeString(attempt.CreatedAt), timeString(attempt.UpdatedAt),
	)
	return err
}

func (s *Store) GetJob(ctx context.Context, id string) (domain.Job, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, team, type, command_or_recipe, workdir, container_image, container_mounts_json, container_workdir, container_mount_home, env_json, gpus, cpus, memory_mb, walltime, checkpoint_mode,
		        checkpoint_dir, resume_command, preemptable, priority_hint, topology_hint, artifacts_dir, desired_state, state,
		        raw_state, reason_code, reason_summary, reason_detail, suggestions_json, created_at, updated_at
		   FROM jobs WHERE id = ?`,
		id,
	)
	return scanJob(row)
}

func (s *Store) ListJobs(ctx context.Context) ([]domain.Job, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, team, type, command_or_recipe, workdir, container_image, container_mounts_json, container_workdir, container_mount_home, env_json, gpus, cpus, memory_mb, walltime, checkpoint_mode,
		        checkpoint_dir, resume_command, preemptable, priority_hint, topology_hint, artifacts_dir, desired_state, state,
		        raw_state, reason_code, reason_summary, reason_detail, suggestions_json, created_at, updated_at
		   FROM jobs ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []domain.Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) UpdateJobState(ctx context.Context, id string, state domain.JobState, rawState string, reason domain.Why) (bool, error) {
	var currentState domain.JobState
	if err := s.db.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id = ?`, id).Scan(&currentState); err == nil {
		if !currentState.CanTransitionTo(state) {
			slog.Warn("invalid state transition (allowing anyway)", "job_id", id, "from", currentState, "to", state)
		}
	}
	suggestionsJSON, err := marshalJSON(reason.Suggestions)
	if err != nil {
		return false, err
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE jobs
		    SET state = ?, raw_state = ?, reason_code = ?, reason_summary = ?, reason_detail = ?, suggestions_json = ?, updated_at = ?
		  WHERE id = ?
		    AND NOT (
		    	state = ?
		    	AND raw_state = ?
		    	AND reason_code = ?
		    	AND reason_summary = ?
		    	AND reason_detail = ?
		    	AND suggestions_json = ?
		    )`,
		state, rawState, reason.ReasonCode, reason.Summary, reason.Detail, suggestionsJSON, time.Now().UTC().Format(time.RFC3339Nano), id,
		state, rawState, reason.ReasonCode, reason.Summary, reason.Detail, suggestionsJSON,
	)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (s *Store) ListActiveJobs(ctx context.Context) ([]domain.Job, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, team, type, command_or_recipe, workdir, container_image, container_mounts_json, container_workdir, container_mount_home, env_json, gpus, cpus, memory_mb, walltime, checkpoint_mode,
		        checkpoint_dir, resume_command, preemptable, priority_hint, topology_hint, artifacts_dir, desired_state, state,
		        raw_state, reason_code, reason_summary, reason_detail, suggestions_json, created_at, updated_at
		   FROM jobs
		  WHERE state NOT IN ('SUCCEEDED', 'FAILED', 'CANCELLED')
		  ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []domain.Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) ListAllocations(ctx context.Context) ([]domain.Allocation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, job_id, node_ids_json, device_ids_json, planner_score, constraints_json, created_at FROM allocations ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var allocations []domain.Allocation
	for rows.Next() {
		var alloc domain.Allocation
		var nodeIDsJSON, deviceIDsJSON, constraintsJSON string
		var created string
		if err := rows.Scan(&alloc.ID, &alloc.JobID, &nodeIDsJSON, &deviceIDsJSON, &alloc.PlannerScore, &constraintsJSON, &created); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(nodeIDsJSON), &alloc.NodeIDs); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(deviceIDsJSON), &alloc.DeviceIDs); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(constraintsJSON), &alloc.Constraints); err != nil {
			return nil, err
		}
		alloc.CreatedAt = mustTime(created)
		allocations = append(allocations, alloc)
	}
	return allocations, rows.Err()
}

func (s *Store) LatestAttemptByJob(ctx context.Context, jobID string) (domain.JobAttempt, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, job_id, attempt, executor, slurm_job_id, raw_state, exit_code, node_list_json, started_at, finished_at, created_at, updated_at
		   FROM job_attempts WHERE job_id = ? ORDER BY attempt DESC, id DESC LIMIT 1`,
		jobID,
	)
	var attempt domain.JobAttempt
	var nodeListJSON, started, finished, created, updated string
	var exit sql.NullInt64
	if err := row.Scan(&attempt.ID, &attempt.JobID, &attempt.Attempt, &attempt.Executor, &attempt.SlurmJobID, &attempt.RawState, &exit, &nodeListJSON, &started, &finished, &created, &updated); err != nil {
		return attempt, err
	}
	if exit.Valid {
		code := int(exit.Int64)
		attempt.ExitCode = &code
	}
	if err := json.Unmarshal([]byte(nodeListJSON), &attempt.NodeList); err != nil {
		return attempt, err
	}
	attempt.StartedAt = mustTime(started)
	attempt.FinishedAt = mustTime(finished)
	attempt.CreatedAt = mustTime(created)
	attempt.UpdatedAt = mustTime(updated)
	return attempt, nil
}

func (s *Store) ListActiveAttempts(ctx context.Context) ([]domain.JobAttempt, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT a.id, a.job_id, a.attempt, a.executor, a.slurm_job_id, a.raw_state, a.exit_code, a.node_list_json, a.started_at, a.finished_at, a.created_at, a.updated_at
		   FROM job_attempts a
		   JOIN jobs j ON j.id = a.job_id
		  WHERE j.state NOT IN ('SUCCEEDED', 'FAILED', 'CANCELLED')
		  ORDER BY a.created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var attempts []domain.JobAttempt
	for rows.Next() {
		var attempt domain.JobAttempt
		var nodeListJSON, started, finished, created, updated string
		var exit sql.NullInt64
		if err := rows.Scan(&attempt.ID, &attempt.JobID, &attempt.Attempt, &attempt.Executor, &attempt.SlurmJobID, &attempt.RawState, &exit, &nodeListJSON, &started, &finished, &created, &updated); err != nil {
			return nil, err
		}
		if exit.Valid {
			code := int(exit.Int64)
			attempt.ExitCode = &code
		}
		if err := json.Unmarshal([]byte(nodeListJSON), &attempt.NodeList); err != nil {
			return nil, err
		}
		attempt.StartedAt = mustTime(started)
		attempt.FinishedAt = mustTime(finished)
		attempt.CreatedAt = mustTime(created)
		attempt.UpdatedAt = mustTime(updated)
		attempts = append(attempts, attempt)
	}
	return attempts, rows.Err()
}

func (s *Store) UpdateAttemptRuntime(ctx context.Context, attemptID int64, rawState string, nodeList []string, exitCode *int, startedAt, finishedAt time.Time) error {
	nodeListJSON, err := marshalJSON(nodeList)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE job_attempts
		    SET raw_state = ?, exit_code = ?, node_list_json = ?, started_at = ?, finished_at = ?, updated_at = ?
		  WHERE id = ?`,
		rawState, exitCode, nodeListJSON, timeString(startedAt), timeString(finishedAt), time.Now().UTC().Format(time.RFC3339Nano), attemptID,
	)
	return err
}

func (s *Store) AddEvent(ctx context.Context, event domain.Event) (domain.Event, error) {
	payloadJSON, err := marshalJSON(event.Payload)
	if err != nil {
		return event, err
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO events (created_at, resource_type, resource_id, reason_code, summary, payload_json) VALUES (?, ?, ?, ?, ?, ?)`,
		time.Now().UTC().Format(time.RFC3339Nano), event.ResourceType, event.ResourceID, event.ReasonCode, event.Summary, payloadJSON,
	)
	if err != nil {
		return event, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return event, err
	}
	event.ID = id
	event.CreatedAt = time.Now().UTC()
	return event, nil
}

func (s *Store) ListEvents(ctx context.Context, limit int) ([]domain.Event, error) {
	query := `SELECT id, created_at, resource_type, resource_id, reason_code, summary, payload_json FROM events ORDER BY id DESC`
	args := []any{}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []domain.Event
	for rows.Next() {
		var event domain.Event
		var created string
		var payloadJSON string
		if err := rows.Scan(&event.ID, &created, &event.ResourceType, &event.ResourceID, &event.ReasonCode, &event.Summary, &payloadJSON); err != nil {
			return nil, err
		}
		event.CreatedAt = mustTime(created)
		if payloadJSON != "" {
			if err := json.Unmarshal([]byte(payloadJSON), &event.Payload); err != nil {
				return nil, err
			}
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) SaveSimulationRun(ctx context.Context, result domain.SimulationResult) error {
	affected, err := marshalJSON(result.AffectedJobs)
	if err != nil {
		return err
	}
	recovered, err := marshalJSON(result.RecoveredJobs)
	if err != nil {
		return err
	}
	failed, err := marshalJSON(result.FailedJobs)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO simulation_runs (id, action, summary, affected_jobs_json, recovered_jobs_json, failed_jobs_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		result.ID, result.Action, result.Summary, affected, recovered, failed, result.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (s *Store) UpsertBenchmark(ctx context.Context, deviceID string, benchmark domain.Benchmark) error {
	payload, err := marshalJSON(benchmark)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO benchmarks (device_id, payload_json) VALUES (?, ?)
		 ON CONFLICT(device_id) DO UPDATE SET payload_json = excluded.payload_json`,
		deviceID, payload,
	)
	return err
}

func (s *Store) CreateCheckpoint(ctx context.Context, cp domain.Checkpoint) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO checkpoints (job_id, path, producer_type, step_label, verified, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		cp.JobID, cp.Path, cp.ProducerType, cp.StepLabel, boolToInt(cp.Verified), time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (s *Store) ListCheckpointsByJob(ctx context.Context, jobID string) ([]domain.Checkpoint, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, job_id, path, producer_type, step_label, verified, created_at FROM checkpoints WHERE job_id = ? ORDER BY created_at DESC`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var checkpoints []domain.Checkpoint
	for rows.Next() {
		var cp domain.Checkpoint
		var verified int
		var created string
		if err := rows.Scan(&cp.ID, &cp.JobID, &cp.Path, &cp.ProducerType, &cp.StepLabel, &verified, &created); err != nil {
			return nil, err
		}
		cp.Verified = verified == 1
		cp.CreatedAt = mustTime(created)
		checkpoints = append(checkpoints, cp)
	}
	return checkpoints, rows.Err()
}

func (s *Store) LatestCheckpoint(ctx context.Context, jobID string) (domain.Checkpoint, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, job_id, path, producer_type, step_label, verified, created_at FROM checkpoints WHERE job_id = ? ORDER BY created_at DESC LIMIT 1`, jobID)
	var cp domain.Checkpoint
	var verified int
	var created string
	if err := row.Scan(&cp.ID, &cp.JobID, &cp.Path, &cp.ProducerType, &cp.StepLabel, &verified, &created); err != nil {
		return cp, err
	}
	cp.Verified = verified == 1
	cp.CreatedAt = mustTime(created)
	return cp, nil
}

func (s *Store) AddGPUHours(ctx context.Context, teamName string, hours float64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE teams SET gpu_hours = gpu_hours + ? WHERE name = ?`, hours, teamName)
	return err
}

func scanJob(scanner interface{ Scan(dest ...any) error }) (domain.Job, error) {
	var job domain.Job
	var envJSON, suggestionsJSON, containerMountsJSON string
	var preemptable, containerMountHome int
	var created, updated string
	if err := scanner.Scan(&job.ID, &job.Name, &job.Team, &job.Type, &job.CommandOrRecipe, &job.Workdir, &job.ContainerImage, &containerMountsJSON, &job.ContainerWorkdir, &containerMountHome, &envJSON, &job.GPUs, &job.CPUs,
		&job.MemoryMB, &job.Walltime, &job.CheckpointMode, &job.CheckpointDir, &job.ResumeCommand, &preemptable, &job.PriorityHint,
		&job.TopologyHint, &job.ArtifactsDir, &job.DesiredState, &job.State, &job.RawState, &job.ReasonCode, &job.ReasonSummary,
		&job.ReasonDetail, &suggestionsJSON, &created, &updated); err != nil {
		return job, err
	}
	job.Preemptable = preemptable == 1
	job.ContainerMountHome = containerMountHome == 1
	if err := json.Unmarshal([]byte(containerMountsJSON), &job.ContainerMounts); err != nil {
		return job, err
	}
	if err := json.Unmarshal([]byte(envJSON), &job.Env); err != nil {
		return job, err
	}
	if err := json.Unmarshal([]byte(suggestionsJSON), &job.Suggestions); err != nil {
		return job, err
	}
	job.CreatedAt = mustTime(created)
	job.UpdatedAt = mustTime(updated)
	if job.Env == nil {
		job.Env = map[string]string{}
	}
	return job, nil
}

func marshalJSON(v any) (string, error) {
	if v == nil {
		return "null", nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func mustTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, raw)
	return t
}

func timeString(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (s *Store) ensureColumn(ctx context.Context, table, column, ddl string) error {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid        int
			name       string
			dataType   string
			notNull    int
			defaultV   sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultV, &primaryKey); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, ddl)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
		return nil
	}
	return err
}

func JoinSQLPlaceholders(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return strings.TrimRight(strings.Repeat("?,", len(items)), ",")
}
