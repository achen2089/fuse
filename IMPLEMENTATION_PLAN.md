# Fuse MVP Implementation Plan

This repo now targets an honest MVP built around a real 16-GPU Slurm slice and a separate faker path for scale-out demos.

## Core stance

- real execution goes through Slurm
- the real cluster path is daemon-free on cluster
- `fuse server --faker` is local-only for simulation
- real inventory discovery comes from Slurm node metadata over SSH
- the live cluster principal is `user44@184.34.82.180`
- many agents may share that same cluster principal
- Fuse owns logical actor identity, job intent, reasoning, state, and collision avoidance above the shared credential
- faker discovery provides scale and demo repeatability
- optional local GPU discovery adds one real node into the model

## Concrete implementation

The concrete implementation is a single Go binary with:

- direct CLI mode for the real Slurm-backed path
- local HTTP+JSON server for faker-backed simulation
- read-only TUI
- SQLite-backed state
- Slurm adapter using `sbatch`, `squeue`, `sacct`, and `scancel`
- SSH-backed execution against `user44@184.34.82.180`
- direct CLI defaults to `user44@184.34.82.180` unless `--ssh-host`, `FUSE_SSH_HOST`, `--faker`, or `--nvml` override that behavior
- Slurm-backed node discovery and observed GPU occupancy from `scontrol show nodes -o`
- actor and request metadata persisted on mutating actions
- idempotency keys, resource versions, and short-lived leases in local state
- remote artifacts, logs, and checkpoints rooted at `/mnt/sharefs/user44`

## Concurrency model

The concurrency model should be explicit:

- `user44` is the shared transport credential for SSH and Slurm
- Fuse should treat that shared credential as authentication, not as the full identity model
- each mutating action should carry an actor ID and request ID
- parallel agents should coordinate through persisted state, idempotent job keys, and action preconditions
- safe parallelism should increase scheduler utilization instead of forcing a single control loop
- long-running orchestration steps should claim short-lived leases
- the event log should record which actor acted, on what object, and why

## Current operational CLI surface

- real path, direct:
  - `fuse submit <job.json>`
  - `fuse run --gpus N -- <cmd>`
  - `fuse jobs`
  - `fuse logs <job-id>`
  - `fuse why <job-id>`
  - `fuse storage`
  - `fuse topo --gpus 8`
  - `fuse topo --job <job-id>`
  - `fuse train --example nanochat ...`
  - `fuse run --gpus 1 -- python makemore.py -i names.txt -o names-demo`
- simulation path:
  - `fuse server --faker`
  - `fuse simulate --addr http://127.0.0.1:9090 ...`

## Current validation workloads

- `makemore` for the fastest single-node smoke and bring-up path
- `nanochat` for the minimal readable distributed demo on the real 16-GPU slice
- Axolotl or LlamaFactory only after the simple path is stable

## Demo surfaces that need to land next

- `fuse status`
- `fuse fabric`
- `fuse nodes`
- `fuse shard`
- `fuse doctor`
- `fuse snapshot --json`
- `possible_actions` on key resources

## Build order

1. domain types, SQLite persistence, actor metadata, and event history
2. direct Slurm-backed CLI path
3. idempotency keys, resource versions, and leases for safe parallel agent operation
4. topology reasoning, sharding plans, `fuse why`, and `fuse snapshot`
5. faker discovery and local simulation server
6. CLI/TUI and docs alignment

The current code follows that structure under `cmd/` and `internal/`.
