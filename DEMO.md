# Fuse Demo Plan

This is the current honest demo run-of-show for the repo as it exists today.

The goal is not to present the aspirational end-state. The goal is to show the strongest live path we actually have, make the product wedge clear, and mark simulation or manual steps explicitly when we cross that boundary.

## Demo Thesis

Fuse is an AI-native orchestrator over Slurm that makes GPU clusters easier for humans and concurrent agents to operate.

For the current MVP, the demo should prove three things:

1. Fuse can reason about a real `16 GPU` cross-node slice.
2. Fuse exposes a better control surface than raw Slurm for humans and agents.
3. Fuse gets users to a real job faster, with less ceremony than a heavier Kubernetes-plus-Volcano path.

## Source Of Truth

Use these documents as the truth hierarchy:

1. `scripts/README.md` for what has actually been validated on the cluster
2. `scripts/workloads/README.md` for workload-specific live commands
3. `README.md` for the broader product story
4. `SLIDE.md` for the short deck framing
5. `VISUALS.md` for the demo diagrams and comparison visuals

If those disagree, prefer the first two.

## What Is Real Today

Live on the real cluster:

- Slurm-backed `fuse status`
- Slurm-backed `fuse nodes`
- Slurm-backed `fuse fabric`
- Slurm-backed `fuse jobs`
- Slurm-backed `fuse why`
- Slurm-backed `fuse logs`
- Slurm-backed `fuse cancel`
- Slurm-backed `fuse checkpoints`
- Slurm-backed topology probing via `fuse topo`
- Hardware-aware sharding plans via `fuse shard`
- `fuse train --example makemore`
- `fuse train --example nanochat --gpus 2 --steps 10 --hold 60`
- Manual `2`-node `nanochat` probe through Slurm with the staged NGC image
- Local `fuse server --faker` plus `fuse simulate` for scale-out stories

Validated cluster facts:

- `user44` is capped by QOS at `16` GPUs, `384` CPUs, and `4000G` memory
- The cluster path is real Slurm over SSH, not a Fuse daemon on the cluster
- The cluster is thin by default, so container-first execution is the credible path
- The staged NGC image is the current reliable distributed demo image
- For the current `2`-node distributed probe, use socket transport, not raw IB

## What Is Not Real Yet

Do not present these as shipped:

- `fuse train --example nanochat --gpus 16` as a fully live Fuse-managed multi-node launcher
- `fuse doctor` on the real CLI
- Cluster-resident `fuse server` on the live path
- Fully automatic checkpoint-aware recovery on the live cluster
- Production-grade multi-agent leases, actor identity, and resource preconditions across every mutating path

Talk about these as direction, target surface, or simulation only.

## Demo Strategy

Use a two-layer story:

1. Live proof on the real cluster
2. Product direction and scale-out through simulation or clearly labeled next-step surfaces

That keeps the demo credible.

## Recommended Run Of Show

This version is designed for `6` to `8` minutes.

### Beat 1: The setup is real

Message:

This is not a toy local demo. We have a real `16 GPU` slice on a real Slurm cluster.

Show:

```bash
./fuse status
./fuse nodes
./fuse fabric
```

Say:

- We are operating on the real cluster path over Slurm
- This slice guarantees a cross-node story
- Fuse is showing topology and allocation shape, not a fake cluster

### Beat 2: The problem changes once training crosses nodes

Message:

Cross-node training is where resource counting stops being enough.

Show:

```bash
./fuse topo --gpus 8
./fuse shard --model llama-70b --gpus 16
```

Say:

- NVLink dominates inside a node
- NIC locality and socket transport matter across nodes
- Fuse can produce a hardware-aware sharding plan against the real B200 shape

Safe line:

Fuse makes sharding easier through hardware-aware plans. Do not say it automatically shards arbitrary training code.

### Beat 3: The live Fuse path is already better than raw Slurm

Message:

Fuse already gives users a cleaner launch and inspection flow on top of Slurm.

Show:

```bash
./fuse train --example makemore --name makemore-demo --steps 200
./fuse jobs
./fuse why makemore-demo
./fuse logs makemore-demo
```

Say:

- This is a real Slurm-backed training job
- Fuse gives job identity, reasoning, and logs in one surface
- This is the “faster time to first job” part of the product

### Beat 4: Prove the current higher-level GPU path

Message:

Fuse can already manage a real multi-GPU training-shaped flow on one node.

Show:

```bash
./fuse train --example nanochat --name nanochat-cancel-proof --gpus 2 --steps 10 --hold 60
./fuse why nanochat-cancel-proof
./fuse cancel nanochat-cancel-proof
./fuse why nanochat-cancel-proof
```

Say:

- This path is real today
- It uses `torchrun --standalone` under the hood
- The key story is cleaner orchestration, not just raw submission

### Beat 5: Be explicit about the current distributed boundary

Message:

The current live multi-node `nanochat` path is real, but it is still a manual Slurm launcher rather than a Fuse-synthesized multi-node launcher.

Show:

```bash
srun --immediate=120 -p priority \
  --nodes=2 --ntasks=2 --ntasks-per-node=1 \
  --gres=gpu:1 --mem=20G --cpus-per-task=4 --time=00:05:00 \
  --container-image=$HOME/fuse-ngc-pytorch-2502.sqsh \
  --container-mount-home \
  bash -lc '
    export NCCL_IB_DISABLE=1
    export NCCL_SOCKET_IFNAME=enp71s0
    export FUSE_RDZV=file:///mnt/sharefs/user44/nanochat-smoke-rdzv-${SLURM_JOB_ID}
    python /mnt/sharefs/user44/fuse-workloads/nanochat_smoke.py --steps 20
  '
```

Say:

- The distributed workload is real
- The orchestration gap is exactly what Fuse is closing
- Today, Fuse gives the reasoning and control surface around that path, and the multi-node launcher is the next step

### Beat 6: Agent-friendly control is the second wedge

Message:

The value is not merely that an agent can shell out to Slurm. The value is safer parallel control when many actors share one cluster principal.

Show:

```bash
./fuse jobs --json
./fuse why makemore-demo --json
```

Say:

- On this cluster, many agents may effectively share `user44`
- Shared credential is transport, not identity
- Fuse is pushing toward actor IDs, request IDs, safer retries, and clearer next actions
- Structured output is how we stop agents from scraping unstable text and colliding

### Beat 7: Simulation is where we go beyond the live slice

Message:

Simulation belongs after the live proof, not instead of it.

Show:

```bash
./fuse simulate --kill-node n1
./fuse simulate --add-nodes 4 --switch leaf-01
```

Say:

- The live path proves the real cluster wedge
- Simulation handles larger-than-live capacity planning and failure scenarios

## Best Demo Narrative

The cleanest story is:

1. Real `16 GPU` slice
2. Real topology and sharding plan
3. Real Fuse-managed jobs on Slurm
4. Real distributed boundary called out honestly
5. Agent-friendly control surface as the second wedge
6. Simulation only after live proof

## Visual Sequence

Use these visuals in this order:

1. `VISUALS.md` Visual 1: same cluster, different surface
2. `VISUALS.md` Visual 2: the real `16 GPU` slice
3. `VISUALS.md` Visual 3: weighted topology, not flat GPU counts
4. `VISUALS.md` Visual 4: many agents, one cluster principal
5. `VISUALS.md` Visual 5: why not Volcano
6. `VISUALS.md` Visual 8: live now vs direction

If time is tight, keep Visuals `1`, `2`, `4`, and `5`.

## Commands To Prepare In Advance

From your laptop:

```bash
./scripts/stage-remote-workloads.sh
./scripts/fuse-smoke.sh
```

Optional cluster recon refresh:

```bash
./scripts/slurm-recon.sh --remote user44@184.34.82.180 --mode quick
./scripts/slurm-smoke.sh --remote user44@184.34.82.180 --mode full
```

If the staged NGC image is missing:

```bash
srun --immediate=120 -p priority --gres=gpu:1 --mem=20G --time=00:20:00 \
  --container-image=docker://nvcr.io/nvidia/pytorch:25.02-py3 \
  --container-save=$HOME/fuse-ngc-pytorch-2502.sqsh \
  bash -lc 'python -c "import torch; print(torch.__version__)"'
```

## Pre-Demo Checklist

- `./fuse` is built locally
- Workload scripts are staged under `/mnt/sharefs/user44/fuse-workloads`
- The staged NGC image exists at `$HOME/fuse-ngc-pytorch-2502.sqsh`
- `./scripts/fuse-smoke.sh` passes
- `./fuse shard --model llama-70b --gpus 16` returns a sane plan
- `./fuse train --example nanochat --gpus 2 --steps 10 --hold 60` still reaches `RUNNING`
- The manual `2`-node `nanochat` Slurm probe still works
- You have screenshots or captured output for `status`, `fabric`, `shard`, `jobs --json`, and the distributed probe

## Backup Plan

If the live cluster is flaky:

1. Show `fuse status`, `fuse nodes`, and `fuse fabric` from captured output
2. Show `fuse shard --model llama-70b --gpus 16` from captured output
3. Use recorded `makemore` and `nanochat` logs
4. Use simulation for the capacity and failure story
5. Be explicit about what is recorded versus live

## What To Say If Challenged

### “Is Fuse replacing Slurm?”

No. Not in the current MVP. Fuse is an orchestration layer over Slurm on the live cluster path.

### “Is the 16-GPU live path fully Fuse-managed?”

Not end-to-end yet. Fuse can reason about the real slice and improve the live control surface today, but the current multi-node `nanochat` launcher is still manual Slurm.

### “Why not Volcano?”

Volcano is the strongest Kubernetes-native comparison, but it still lives at the Kubernetes abstraction layer and requires a much heavier setup path before the first real job runs.

### “What is the real wedge?”

Two wedges:

- topology-aware orchestration over a real cross-node slice
- a safer control surface for humans and concurrent agents sharing the same cluster credential

## One-Line Close

Same Slurm cluster, better AI-native orchestration surface: real topology, easier sharding, safer control for humans and agents, and a faster path from intent to running work.
