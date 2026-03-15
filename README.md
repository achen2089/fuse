# Fuse

**GPU-native job scheduler — topology-aware training, auto-sharding, fair-share scheduling, and agent-safe parallel control in a single binary.**

## Current MVP Status

Fuse is currently implemented as an **opinionated CLI and UX layer over Slurm**, shaped for both human operators and concurrent agents. For the current MVP, `fuse server` is only in scope for `--faker` simulation. We are not running a Fuse server or agent path on the real cluster.

On the live cluster, multiple tools or agents may operate through the same shared Slurm principal, `user44`. That shared credential is only the transport layer. Fuse should carry logical actor identity, idempotency, and collision-avoidance above it.

What is real today:

- Local CLI state and command wrappers
- Faker-based cluster discovery for demos and simulation
- Optional local GPU discovery via `nvidia-smi`
- Real workload submission through Slurm
- `fuse submit`, `fuse run`, example-driven `fuse train`, `fuse shard`, `fuse logs`, `fuse why`, `fuse storage`, `fuse topo`, and `fuse simulate`
- SSH-backed Slurm execution for the FluidStack cluster login node
- Remote shared storage rooted at `/mnt/sharefs/user44` for workdirs, artifacts, and checkpoints

What is in active scope for the `16 GPU` hackathon demo:

- Slurm-backed `fuse status`, `fuse fabric`, and `fuse nodes`
- Hardware-aware sharding-plan output via `fuse shard`
- Single-node example launch flow through `fuse train`
- Manual `2`-node `nanochat` path kept as plain Slurm until the multi-node launcher is real
- `fuse doctor`, `fuse why`, and checkpoint/resume demo paths
- `fuse teams` for quota / burst / fair-share storytelling
- Agent-safe control surfaces for many agents sharing the same cluster principal: `--json`, idempotent retries, explicit next actions, and low-collision operator / agent workflows
- Validation workloads kept intentionally simple: `makemore` for the fastest smoke path and `nanochat` for the live distributed run
- Axolotl or LlamaFactory only if time remains after the simple path is stable
- Local `fuse server --faker` plus `fuse simulate` for scale-out and failure stories beyond the live slice

The rest of this README uses that hackathon-demo target surface. When there is a question about what is live on the real cluster versus local simulation, this section and the operations guide are the source of truth.

What is not built yet:

- Native Fuse executor
- Cluster-resident Fuse server / agent workflow
- Distributed control plane / Raft / HA
- Remote agent mesh
- Production-grade automatic recovery

For the FluidStack hackathon, the current assumption is a **guaranteed 16-GPU slice** for the live path, with larger-cluster behavior shown through the faker and simulator.

Example faker startup for the simulation path:

```bash
fuse server --faker
```

## Getting Started

The canonical binary name is `fuse`.

For a repo-local workflow:

```bash
make build
./fuse --help
./fuse --faker
```

For an installed workflow:

```bash
make install
fuse --help
fuse --faker
```

Compatibility note:

- `./.bin/fuse-live` is a generated wrapper for older scripts and forwards to the canonical `./fuse` binary after `make build`.
- Treat `./fuse` or installed `fuse` as the primary entrypoint.

## Current Operational Surface

Real cluster path:

```bash
ssh user44@184.34.82.180
salloc -p priority --gpus=16 --time=04:00:00
scontrol show hostnames "$SLURM_JOB_NODELIST"
srun --ntasks-per-node=1 nvidia-smi topo -m
```

No `fuse server` runs on the cluster in this flow. The real path is Slurm-backed execution. `fuse server --faker` remains local-only for simulation and larger-than-live demos.

The direct CLI now defaults to the current live SSH target: `user44@184.34.82.180`. Override it with `--ssh-host` or `FUSE_SSH_HOST` when needed.

Submit an ad hoc real job through the Slurm-backed flow:

```bash
fuse run --gpus 1 --time 00:05:00 -- /bin/true
fuse jobs
fuse logs <job-id>
fuse storage
fuse topo --gpus 8
fuse topo --job <job-id>
```

Use the higher-level workload and sharding sugar:

```bash
fuse shard --model llama-70b --gpus 16
fuse train --example makemore --steps 80
fuse train --example nanochat --gpus 2 --steps 10 --hold 60
```

What is validated on the live cluster right now:

- `fuse shard --model llama-70b --gpus 16` returns a live B200-backed plan of `TP=8 PP=2 DP=1`
- `fuse train --example makemore` submits and completes on the staged runtimes
- `fuse train --example nanochat --gpus 2 --hold 60` launches a real `torchrun --standalone` job, reaches `RUNNING`, and cancels cleanly to `CANCELLED`
- `fuse train --example axolotl-probe` is useful as a runtime probe, and currently fails on the staged NGC image because `axolotl` is not installed there

Run the end-to-end live Fuse smoke with the validated public PyTorch image:

```bash
./scripts/fuse-smoke.sh
```

That path uses the local `fuse` binary, the default SSH target, submits one real GPU container job, and verifies `fuse jobs`, `fuse why`, and `fuse logs` against the same Fuse-managed job.

Simulation path:

```bash
fuse server --faker
fuse simulate --submit --model llama-405b --gpus 64
```

Submit a canonical JSON `JobSpec` file:

```bash
cat > job.json <<'EOF'
{
  "name": "smoke-json",
  "team": "default",
  "type": "run",
  "command_or_recipe": "/bin/true",
  "gpus": 1,
  "cpus": 4,
  "memory_mb": 16384,
  "walltime": "00:05:00"
}
EOF

fuse submit job.json
```

---

## The story

Every GPU scheduler today was built for a world before large-scale AI training. Slurm was built for HPC batch jobs in the 90s. Kubernetes was built for stateless web services in the 2010s. Both treat GPUs as afterthoughts — opaque resource counters bolted on through plugins and CRDs.

Fuse starts from a different question: **what would a job scheduler look like if GPUs, training runs, and model-aware scheduling were the design center — not an addon?**

The answer has six primitives.

And one operating stance: many humans and agents should be able to share one cluster principal and still operate safely in parallel.

---

## The mental model

```
┌─────────────────────────────────────────────────────┐
│                     CLUSTER                          │
│  The physical world. Nodes, GPUs, NICs, cables.      │
│  Fuse discovers it. You don't configure it.          │
│                                                      │
│  ┌────────────────────────────────────────────────┐  │
│  │                  FABRIC                         │  │
│  │  The network topology. Switches, bandwidth      │  │
│  │  tiers, NVLink domains. First-class, visible,   │  │
│  │  and the scheduler reasons about it.            │  │
│  └────────────────────────────────────────────────┘  │
│                                                      │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐          │
│  │   TEAM   │  │   TEAM   │  │   TEAM   │          │
│  │ ML Res.  │  │   NLP    │  │  Vision  │          │
│  │ quota:32 │  │ quota:24 │  │ quota:8  │          │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘          │
│       │              │              │                │
│  ┌────┴────┐    ┌────┴────┐    ┌───┴─────┐         │
│  │  JOBS   │    │  JOBS   │    │  JOBS   │         │
│  │ train   │    │ serve   │    │ eval    │         │
│  │ finetune│    │ train   │    │finetune │         │
│  └────┬────┘    └────┬────┘    └────┬────┘         │
│       │              │              │                │
│  ┌────┴──────────────┴──────────────┴────┐          │
│  │              MODELS                    │          │
│  │  llama-70b: 80 layers, 64 heads       │          │
│  │  mistral-7b: 32 layers, 32 heads      │          │
│  │  Carry their hardware requirements.   │          │
│  └───────────────────────┬───────────────┘          │
│                          │                           │
│  ┌───────────────────────┴───────────────┐          │
│  │            CHECKPOINTS                 │          │
│  │  First-class. Tracked, verified,       │          │
│  │  garbage-collected. The connective     │          │
│  │  tissue between jobs across time.      │          │
│  └────────────────────────────────────────┘          │
└─────────────────────────────────────────────────────┘
```

### Cluster

The physical world. Nodes, GPUs, NICs, PCIe topology. Fuse discovers it through hardware probing — NVML for NVIDIA, `rocm-smi` for AMD. You don't write a config file describing your hardware. Fuse reads it.

```bash
fuse status
# 9 nodes, 65 GPUs, 2 leaf switches, 1 spine
# 49 allocated, 16 idle, 0 dead
```

### Fabric

The network topology as a first-class object. Not hidden like K8s networking. Not ignored like Slurm. You can see it, query it, and the scheduler reasons about it every time it places a job.

```bash
fuse fabric
#                     ┌────────────┐
#                     │   spine    │
#                     │ (100 Gbps) │
#                     └──┬──────┬──┘
#                        │      │
#           ┌────────────┘      └────────────┐
#      ┌────┴─────┐                    ┌─────┴────┐
#      │ leaf-01  │                    │ leaf-02  │
#      │ 400 Gbps │                    │ 400 Gbps │
#      └┬──┬──┬──┬┘                    └┬──┬──┬──┬┘
#       n1 n2 n3 n4                    n5 n6 n7 n8 n9★
#       H100×8 each                    H100×8     real

fuse fabric --json   # structured for AI agents / scripts
```

Three bandwidth tiers. Fuse knows all of them:

| Tier | Bandwidth | Where |
|------|-----------|-------|
| Intra-node NVLink | 900 GB/s | GPU ↔ GPU inside a node |
| Same-switch IB | 400 Gbps | Nodes under the same leaf switch |
| Cross-spine IB | ~100 Gbps | Nodes on different leaf switches (4:1 oversubscribed) |

Slurm sees: "8 nodes with 8 GPUs each." Fuse sees the weighted graph.

### Teams

The organizational unit. Not K8s namespaces (too generic). Not Slurm partitions (too rigid). A team has a GPU quota, a fair-share score, and jobs. Teams share idle capacity automatically — burst allocation fills gaps, checkpoint-aware preemption reclaims them.

```bash
fuse team create ml-research --quota 32
fuse team create nlp --quota 24
fuse team create vision --quota 8

fuse teams
# TEAM          QUOTA  USED  BURST  PENDING  FAIR-SHARE
# ml-research   32     16    0      0        0.50
# nlp           24     24    8      2        1.15
# vision        8      8     0      0        1.00
```

Three mechanisms, all automatic:

- **Burst.** NLP's jobs spill into ml-research's idle capacity. Marked preemptable.
- **Preemption.** ml-research needs GPUs back → checkpoint signal → grace period → free → requeue NLP from checkpoint.
- **Fair-share.** GPU-hours tracked. Underusers get priority boost. No team monopolizes burst.

### Jobs

Industry-standard term. But in Fuse, jobs have a *type* that determines scheduling behavior, failure handling, and what metrics matter.

| Type | Verb | Scheduling | Failure | Priority |
|------|------|-----------|---------|----------|
| Train | `fuse train` | Gang (all-or-nothing), same-switch | Checkpoint recovery | Normal |
| Serve | `fuse serve` | Placement-aware, fault-isolated replicas | Auto-restart, zero downtime | High |
| Eval | `fuse eval` | Bin-packing, backfill into gaps | Retry | Low |
| Finetune | `fuse finetune` | Gang, topology-aware | Checkpoint recovery | Normal |

Slurm has one verb: `sbatch`. Every workload gets the same treatment.

### Models

First-class resources in the scheduler. Not just "a job that happens to use a model" but "Fuse knows llama-70b exists, knows it's 140GB, knows TP must divide 64 heads." Register once, use everywhere.

```bash
fuse models
# MODEL        PARAMS  LAYERS  HEADS  BF16 MEM   MIN GPUs
# llama-7b     7B      32      32     14 GB      1
# llama-13b    13B     40      40     26 GB      1
# llama-70b    70B     80      64     140 GB     4 (TP)
# llama-405b   405B    126     128    810 GB     16 (TP+PP)
# mistral-7b   7B      32      32     14 GB      1
# mixtral-8x7b 47B     32      32     94 GB      4
# qwen-72b     72B     80      64     144 GB     4 (TP)

# Custom models:
fuse model register my-model --params 30e9 --layers 48 --heads 32
```

When you say `fuse train --model llama-70b --nodes 2`, Fuse looks up the model, computes sharding, checks memory, and generates the full job config. The model carries its requirements.

### Checkpoints

Promoted from "a file the training script writes" to a first-class resource. Fuse tracks them, verifies them, uses them for recovery, and garbage-collects old ones.

```bash
fuse checkpoints
# JOB             LATEST         AGE     SIZE    VERIFIED
# llama-finetune  step-8400      4m      2.1 GB  ✓
# bert-pretrain   step-12000     12m     890 MB  ✓
# vit-large       step-3200      38m     1.4 GB  ✓

fuse checkpoints --job llama-finetune
# step-8400   4m ago    2.1 GB  ✓ verified
# step-8100   34m ago   2.1 GB  ✓ verified
# step-7800   64m ago   2.1 GB  ✓ verified (auto-gc after 3 kept)
```

A failed job's checkpoint is a new job's starting point. Fuse handles the connection — no manual `--resume_from_checkpoint` flag editing. This is what makes failure recovery automatic instead of manual.

---

## Failure is a primitive, not an edge case

Slurm treats failure as "node went down, mark it DOWN, admin fixes it." That's reactive. Fuse is proactive, granular, and automatic.

### Proactive: prevent failures before they happen

```bash
fuse doctor cluster
# ⚠ node-03 GPU 6: 847 ECC errors in 24h (threshold: 100) — recommend drain
# ⚠ leaf-02 → spine: 92% bandwidth utilization — congestion risk
# ⚠ node-07 GPU 2: temperature 89°C (threshold: 85°C) — throttling likely
# ✓ 8/9 nodes healthy
# ✓ All checkpoints verified
# ✓ No silent NCCL fallbacks detected

fuse doctor job-abc123
# ⚠ NCCL using TCP on ranks 2-3 (expected IB 400 Gbps, getting ~10 Gbps)
# ⚠ Pipeline bubble 50% — consider TP=8 PP=1 DP=2 for this model
# ⚠ GPU 5 on node-02: ECC trending (12 errors/hr, accelerating)
# ✓ Checkpoints healthy (last: 4 min ago)
# ✓ Memory: 54% (headroom OK)
# ✓ All ranks responsive
```

Fuse watches ECC error rates, temperature trends, bandwidth degradation. When a GPU is heading toward failure, Fuse can preemptively drain that node and migrate jobs *before* the crash. Slurm can't do this because GPU health isn't part of the scheduler's world model.

### Granular: per-GPU, not per-node

If 1 GPU in an 8-GPU node dies, Slurm marks the entire node DOWN. 7 healthy GPUs wasted.

Fuse knows the per-device topology. It marks 1 GPU dead, keeps the other 7 running, and only reschedules the affected job around the failed device. The node stays in service at 7/8 capacity.

### Automatic: checkpoint-aware recovery

```
GPU fault detected
  → 15s detection (missed heartbeats)
  → Check: does checkpoint exist?
      yes → RECOVERING → verify checkpoint → requeue PENDING → schedule on healthy nodes → RUNNING
      no  → FAILED → notify team
  → 30 seconds total, zero humans

Slurm: job vanishes from squeue. Admin SSHs. Checks dmesg. Checks nvidia-smi.
Researcher checks if checkpoint is recent. Edits sbatch. Resubmits. Waits.
15-60 minutes. If nobody notices: GPU sits idle.
```

### Cascading: smart recovery decisions

If a job spans 4 nodes and 1 dies, Fuse doesn't blindly restart. It considers:

- How old is the checkpoint? (5 min → restart cheap, 2 hours → expensive)
- Is the failed node likely to recover? (reboot vs hardware failure)
- Is there capacity to reschedule? (if cluster is full, waiting might be cheaper)
- What's the estimated cost of restart vs wait?

Then it picks the cheapest option and explains why in the event log.

### Simulated: practice failure safely

```bash
fuse simulate --kill node-03
# → Shows exactly which jobs are affected
# → Shows which checkpoints would be used
# → Shows where jobs would be rescheduled
# → Shows estimated recovery time
# → Nothing actually happens. It's a dry run.

fuse simulate --kill-rack rack-01
# → "4 nodes lost. 3 jobs affected. 2 have checkpoints (recovery ~30s).
#    1 has no checkpoint (FAILED). 24 GPUs lost. Remaining capacity: 41 GPUs.
#    Jobs llama-train and bert-ft can restart on leaf-02.
#    Job vit-large cannot — needs 16 GPUs, only 8 available on leaf-02."
```

Capacity planning meets chaos engineering. Run this before you buy hardware, before you schedule a big job, before you go on vacation.

---

## The faker is a superpower

Now that the core demo can run on 16 real GPUs, the faker shifts from core proof to support system: capacity planner, training ground, and demo engine for stories bigger than the live slice.

### Capacity planning

```bash
# "We're buying 4 more H100 nodes. Where should they go?"
fuse simulate --add-nodes 4 --model H100 --switch leaf-01
# → Replay current job mix
# → "Adding 4 nodes to leaf-01: utilization drops from 89% to 62%.
#    Jobs currently cross-spine can move intra-switch. Estimated 15% throughput gain."

fuse simulate --add-nodes 4 --model H100 --switch leaf-03-new
# → "New leaf-03: adds 32 GPUs but creates another spine hop.
#    Jobs spanning leaf-01 + leaf-03 get 100 Gbps (4× slower than same-switch)."
```

### What-if scheduling

```bash
# "What if we submitted a 64-GPU job right now?"
fuse simulate --submit --model llama-405b --gpus 64
# → "Cannot schedule: only 41 GPUs available. Would need to preempt
#    2 burst jobs (nlp/bert-ft, 16 GPUs) and wait for eval-mmlu to
#    complete (4 GPUs, ~8 min remaining). Estimated start: 12 min."
```

### Safe AI agent training

Run your AI ops scripts against the faker before they touch production. Replay real failure scenarios from logs. The faker behaves identically to real hardware from the API perspective.

---

## Designed for humans and concurrent agents alike

Not a protocol. Not a plugin. Just design decisions that make every interaction structured, explained, predictable, and safer under concurrent control.

This is a product wedge, not a side-effect. As more teams use agents for triage, launch, recovery, and cluster hygiene, the scheduler has to be legible and low-collision under concurrent automation.

### Many agents, one cluster principal

On this cluster, the shared SSH and Slurm identity is `user44`. That is fine. The Unix username is the transport credential, not the real identity model. Fuse should let multiple agents operate in parallel under that shared principal while still carrying actor IDs, request IDs, and coordination state above it.

### Agent-safe scheduling

The bar is not "an agent can call the CLI." The bar is "multiple humans and agents can inspect, submit, retry, and recover work on the same cluster without colliding."

Current building blocks:

- `--json` output on the main control surfaces
- `possible_actions` on resources so agents do not guess valid next steps
- Idempotent retry semantics on mutating actions
- Per-action metadata so the real actor can be tracked even when the transport credential is shared
- `fuse why` and `fuse snapshot` for inspectability instead of regex-parsing cluster text

Why this matters:

- Slurm-style text interfaces force agents to parse unstable output
- Retry often means duplicate submission instead of safe replay
- Two automations can act on stale reads without any explicit precondition checks
- Humans have weak visibility into which automated system made which decision
- If only one agent can safely act at a time, the scheduler stays under-utilized

Primitives to harden next:

- Stable job keys to deduplicate submission across parallel agents
- Resource versions and preconditions on mutating actions
- Short-lived leases or claims for long-running orchestration steps
- Clear ownership and audit history by actor and request, not just by Unix user

### Every command speaks JSON

```bash
fuse jobs                  # pretty table for humans
fuse jobs --json           # structured for scripts / AI agents
```

Same data, two presentations. An AI agent never has to parse `squeue` output with regex.

### Every resource says what you can do next

```bash
fuse job abc123 --json | jq .possible_actions
# ["cancel", "checkpoint", "priority", "profile"]
```

No memorizing state machines. The API tells you what's valid right now. Slurm never tells you what actions are valid — you try something and get a cryptic error or silent success.

### Every action is idempotent

Submit the same job name twice → returns existing job, no duplicate. Cancel an already-cancelled job → success. Drain an already-drained node → no-op.

An AI agent that isn't sure if its last action succeeded can safely retry. With Slurm, `sbatch` twice = two jobs. Idempotency is invisible but it's what makes reliable automation possible.

### `fuse why` — explained decisions

```bash
fuse why abc123 pending
# Reason: insufficient_gpus_on_same_switch
# Detail: Job requests 16 GPUs with topology=same_switch. Leaf-01 has 8 free
#         (need 16). Leaf-02 has 12 free (need 16). No single switch has capacity.
# Suggestions:
#   1. Wait ~45 min (llama-train on leaf-01 completing, frees 16 GPUs)
#   2. Relax topology: fuse priority abc123 --topology any (runs now, 4× slower spine)
#   3. Reduce GPUs: 8 GPUs fits on leaf-02 now (TP=8 PP=1)
```

Compare to Slurm's `(Priority)`. Fuse says why, gives options, estimates when things change.

### `fuse snapshot` — full world state, one call

```bash
fuse snapshot --json > cluster-state.json
# Complete: nodes, GPUs, fabric, topology, jobs, teams, events, checkpoints.
# One call instead of: sinfo + squeue + sacct + scontrol + nvidia-smi + sshare
```

### Errors that teach

```bash
# Slurm:
# "sbatch: error: Batch job submission failed: Invalid generic resource (gres) specification"
# (What's wrong? Which gres? What's right?)

# Fuse:
fuse train --model llama-70b --gpus 4
# Error: insufficient_gpu_memory
# Detail: llama-70b needs 140 GB weights (BF16). With TP=4, per-GPU memory is
#         35 GB weights + 120 GB optimizer = 65 GB/GPU. Activations push past 80 GB.
# Fix: fuse train --model llama-70b --gpus 8  (TP=8, 43 GB/GPU, fits)
# Alt: fuse finetune --model llama-70b --method lora --gpus 4  (LoRA = 2 GB trainable)
```

### Collision avoidance is a feature

Most "agent support" stories are really just automation wrappers over brittle text commands. Fuse should be better than that. If two agents share the same cluster credential and both try to act, the system should bias toward safe retries, explicit preconditions, leases, and clean failures instead of duplicate jobs or accidental interference.

### `fuse cost` — know what you're spending

```bash
fuse cost llama-finetune
# 16 GPUs × 2.3 hours = 36.8 GPU-hours
# Idle time: 4.2 GPU-hours (pipeline bubble)
# Efficiency: 88.6%
# Estimated cloud cost: $147.20 (at $4/GPU-hr H100 spot)

fuse cost --team ml-research --period 7d
# This week: 892 GPU-hours across 14 jobs
# Efficiency: 76.4% (3 jobs had >30% pipeline bubble)
# Suggestion: 2 jobs would benefit from DP=2 over PP=2
```

---

## The Slurm side-by-side

We're on a real Slurm cluster with a 16-GPU allocation. That changes the proof. Fuse is no longer showing "1 real GPU plus a fake cluster." The live path is a real distributed workflow inside a real slice of the cluster, and that path does **not** run `fuse server` on the cluster. The simulator still matters, but it moves to the scale-out and chaos-planning part of the story.

```bash
# Ask Slurm for the arena we actually care about
salloc -p priority --gpus=16 --time=04:00:00

# Check what shape Slurm actually gave us
scontrol show hostnames "$SLURM_JOB_NODELIST"
srun --ntasks-per-node=1 nvidia-smi topo -m

# No Fuse daemon runs on this allocation for the current MVP.
# Discovery and placement logic are driven from Slurm-visible topology and local simulation data.
# Best case: 2 x 8-GPU nodes with NVLink inside each node and IB between them.
# If the slice is fragmented, Fuse says so immediately and plans around it.
```

### Distributed launch: 40 lines vs 1 line

```bash
# ═══ SLURM ═══                          # ═══ FUSE ═══

cat << 'EOF' > nanochat.sbatch           fuse train \
#!/bin/bash                                --example nanochat \
#SBATCH --job-name=nanochat-demo           --repo karpathy/nanochat \
#SBATCH --nodes=2                          --gpus 16 \
#SBATCH --gpus-per-node=8                  --gpus 16
#SBATCH --cpus-per-gpu=8                   --checkpoint-every 300s
#SBATCH --mem=0                            # That's it.
#SBATCH --time=02:00:00                    # Topology inferred.
#SBATCH --output=logs/%x-%j.out            # Logs stream automatically.
#SBATCH --error=logs/%x-%j.err             # Checkpoint wiring included.
                                           # Rendezvous + NCCL auto-set.
module load cuda/13.1                      # Failure → auto-recovery.
module load nccl
source activate train

MASTER_ADDR=$(scontrol show hostnames "$SLURM_JOB_NODELIST" | head -n 1)
MASTER_PORT=29500

export NCCL_DEBUG=INFO
export NCCL_IB_HCA=mlx5_0,mlx5_1
export NCCL_SOCKET_IFNAME=ib0
export NCCL_NET_GDR_LEVEL=5
export TORCH_NCCL_ASYNC_ERROR_HANDLING=1
export OMP_NUM_THREADS=8
export TOKENIZERS_PARALLELISM=false

srun bash ./demo/run-nanochat.sh \
  --nnodes="$SLURM_JOB_NUM_NODES" \
  --nproc_per_node=8 \
  --rdzv_backend=c10d \
  --rdzv_endpoint="${MASTER_ADDR}:${MASTER_PORT}" \
  --checkpoint_every=100
EOF
mkdir -p logs
sbatch nanochat.sbatch
```

### Validation path stays simple

```bash
# Fastest smoke path
python makemore.py -i names.txt -o names-demo

# Live distributed demo path
# keep it minimal and readable with nanochat on the real 16-GPU slice
```

### Fair-share: `sshare` vs `fuse teams`

```bash
# Slurm:
sshare -u user44
# Account  User    RawShares  NormShares  RawUsage  EffectvUsage  FairShare
# myacct   user44  100        0.010000    284672    0.023456      0.456789
# (what do these numbers mean?)

# Fuse:
fuse teams
# TEAM          QUOTA  USED  BURST  PENDING  FAIR-SHARE  GPU-HOURS(7d)
# ml-research   32     16    0      0        0.50        892
# nlp           24     24    8      2        1.15        1204
# vision        8      8     0      0        1.00        340
```

### Multi-node NCCL is the live cluster boundary

```bash
# Today:
#   Fuse owns discovery, topology reasoning, sharding plans, job identity, and state
#   The current 2-node nanochat launcher is still manual Slurm
# Next step:
#   Fuse should synthesize the multi-node launcher itself

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

---

## What the 16 real GPUs do

Not proof-of-life. Real distributed work.

**Real topology discovery.** Fuse reads the actual 16-GPU slice Slurm handed us. If it's a clean `2 x 8`, Fuse sees two NVLink islands and one IB hop. If it's fragmented, Fuse surfaces that immediately instead of hiding it behind device IDs.

**Real distributed boundary.** The cluster path is real, and the current `2`-node `nanochat` probe is real, but the multi-node launcher is still manual Slurm today. That boundary is part of the honest MVP story.

**Real auto-sharding.** `fuse shard` recommends against the live allocation, not a synthetic cluster. The recommendation can be checked against measured bandwidth and device topology on the spot.

**Real checkpoint and recovery direction.** Checkpoints, restart, and explanation are core parts of the product story, but the live demo should present them as partial surface plus next-step direction unless the exact path has been validated on the cluster first.

**Real validation workflow.** `makemore` is the quick smoke path because it is tiny and easy to reason about. `nanochat` is the live distributed path because it is still minimal enough to debug in public while exercising real multi-node launch.

**Simulated scale-out.** The faker still matters, but now for the stories that exceed the live slice: rack failures, 64-GPU planning, and alternate cluster shapes.

---

## Auto-sharding

```bash
fuse shard --model llama-70b --gpus 16

# On a clean 2 x 8-GPU slice:
# Recommended: TP=8 PP=2 DP=1
#   TP=8 stays inside each node's NVLink island
#   PP=2 crosses the IB link once
#   Memory/GPU: 43.2 / 80 GB (54%)
#   Bubble: 50%
#
# Alternative: TP=8 PP=1 DP=2
#   Better pipeline efficiency, more gradient sync over IB
#
# Bench data: calibrated from the live allocation with `fuse bench`
# If the slice is fragmented, Fuse explains the constraint and suggests the next-best plan

fuse shard --model llama-405b --nodes 8
# → TP=8 PP=4 DP=2 (810 GB model, needs 4+ PP stages)
```

Logic: memory budget → TP sized to NVLink domain and attention heads → PP minimized for bubble → DP fills remaining → bandwidth cost per tier → hardware calibration from `fuse bench`.

---

## The TUI

```
╭─ fuse ──────────────────────────────────────────────╮
│  2 nodes  16 GPUs  16 alloc  0 idle  0 dead         │
│  1 running  1 pending  1 recovering                  │
╰─────────────────────────────────────────────────────╯

  n1  [████████]  8/8  nanochat-demo rank 0-7        47°C
  n2  [████████]  8/8  nanochat-demo rank 8-15       45°C

  14:23  → nanochat-demo: n1,n2 (topology-aware placement)
  14:31  ★ bench n1,n2: live topology calibrated
  14:52  ✗ rank 11 lost heartbeat → ckpt step-4800 verified
  15:01  ↻ nanochat-demo resumed on healthy devices
  15:10  … eval-mmlu queued (needs 4 GPUs, waiting on preemptable capacity)
```

---

## The full CLI

The examples below mix the live CLI and the target hackathon surface. Today, `fuse train` is real for staged single-node examples like `makemore`, `nanochat`, and `axolotl-probe`; the multi-node `nanochat --gpus 16` form remains the intended next step, not a completed live launcher.

### Workload verbs

```bash
fuse train     --example nanochat --gpus 16
fuse run       --gpus 1 -- python makemore.py -i names.txt -o names-demo
fuse serve     --model llama-7b --gpus 1 --engine vllm --port 8000
fuse eval      --model llama-7b --gpus 4 --sweep --param benchmark=mmlu,arc,hellaswag
fuse bench     --type compute     # or: nccl, memory, health
fuse run       --gpus 8           # interactive shell inside the allocation
```

### Introspection

```bash
fuse status                     # cluster overview
fuse fabric                     # topology with bandwidth
fuse nodes                      # node table with health, GPUs, temp
fuse jobs                       # job table with type, status, checkpoint
fuse models                     # registered models and requirements
fuse teams                      # quotas, usage, fair-share
fuse checkpoints                # tracked checkpoints across jobs
fuse events                     # scheduling event stream with rationale
fuse logs <job> [--rank N]      # per-rank log streaming
fuse metrics <job>              # GPU util, memory, throughput, comm overhead
fuse cost <job|--team T>        # GPU-hours, efficiency, estimated cost
```

### Intelligence

```bash
fuse shard     --model llama-70b --gpus 16     # sharding recommender
fuse doctor    cluster                          # cluster health diagnosis
fuse doctor    <job>                            # job performance diagnosis
fuse why       <job> pending                    # explain scheduling decision
fuse snapshot  --json                           # full world state, one call
fuse simulate  --kill node-03                   # dry-run failure
fuse simulate  --add-nodes 4 --switch leaf-01   # capacity planning
fuse simulate  --submit --model llama-405b --gpus 64  # what-if scheduling
```

### Control

```bash
fuse submit job.yaml            # YAML for power users / CI
fuse cancel <job>               # checkpoint + grace + cancel
fuse checkpoint <job>           # trigger immediate checkpoint
fuse priority <job> high        # bump priority
fuse drain <node>               # graceful drain
fuse undrain <node>             # bring back
fuse kill-node <node>           # simulate failure / recovery drill
fuse team create <n> --quota N  # new team
fuse model register <n> --params P --layers L --heads H  # custom model
```

---

## What to build

```
fuse
├── cmd/
│   ├── cli.go            # all commands
│   └── server.go         # faker-only simulation server
├── pkg/
│   ├── slurm/            # sbatch/squeue/sacct/scancel wrappers
│   ├── ssh/              # login-node transport
│   ├── faker/            # simulated cluster for demos
│   ├── state/            # local state, resource versions, idempotency keys
│   ├── leases/           # action claims, ownership, collision avoidance
│   ├── scheduler/        # gang + bin-packing + topology
│   ├── fabric/           # switch graph, bandwidth tiers
│   ├── discovery/        # slurm allocation + nvidia (NVML)
│   ├── launch/           # rendezvous, env injection, distributed start
│   ├── jobs/             # state machine, job types
│   ├── models/           # model registry, profiles
│   ├── checkpoints/      # tracking, verification, GC
│   ├── teams/            # quotas, burst, preemption, fair-share
│   ├── shard/            # TP/PP/DP recommender
│   ├── doctor/           # health diagnosis, silent-fallback detection
│   ├── bench/            # GPU profiling
│   ├── simulator/        # dry-run failures, capacity planning
│   ├── cost/             # GPU-hours, efficiency tracking
│   └── types/            # Node, Device, Job, Team, Model, Checkpoint
└── main.go
```

---

## Timeline (1-day hackathon)

| Hour | What | Done when |
|------|------|-----------|
| 0–1 | Slurm bootstrap + real discovery + `status`/`fabric`/`nodes` | Fuse sees the actual 16-GPU allocation |
| 1–3 | Distributed launcher + scheduler + `fuse train` | 16-rank job is placed and RUNNING without a cluster-resident Fuse daemon |
| 3–4 | `fuse shard` + `fuse bench` on the live slice | Shard reflects measured topology |
| 4–5 | Real 16-GPU `nanochat` run + checkpoints | Loss moves, checkpoints land, resume works |
| 5–6 | `fuse doctor` + `fuse why` + failure drill | Diagnostics explain a real interruption |
| 6–7 | Local faker for >16 GPUs + rehearsal | `fuse server --faker` and `fuse simulate` cover rack fail / 64-GPU cases |

### Cut list (drop from bottom)

1. Slurm bootstrap + real discovery — **must ship**
2. Distributed launcher + scheduler — **must ship**
3. Job state machine + checkpoints — **must ship**
4. `fuse shard` + model registry
5. `fuse doctor` + `fuse why`
6. Failure drill + recovery on the live slice
7. Slurm side-by-side
8. `fuse bench`
9. Local faker + `fuse simulate` (dry-run failures, capacity planning)
10. Fair-share demo inside the slice
11. `fuse serve`
12. TUI (React artifacts as fallback)
13. `fuse cost`
14. `fuse snapshot --json`

### Cross-cutting requirement: agent-safe ops

- Stable job identity so retries and duplicate submissions collapse cleanly
- Idempotent mutating actions wherever retry is expected
- Resource versions or preconditions so stale agents fail safely
- Actor and request identity above the shared `user44` cluster principal
- `--json`, `possible_actions`, `fuse why`, and `fuse snapshot` as the inspection surface
- Clear event history so humans can see which agent or operator acted

This is not separate from the scheduler story. It is part of the scheduler story. If Fuse is supposed to be the control plane for AI work, it needs to stay coherent when humans, scripts, and multiple agents all touch the same cluster.

---

## Demo script (7 min)

The more honest current run-of-show lives in `DEMO.md`. The table below is the high-level target shape and should be read with that boundary in mind.

| Time | Beat | Show |
|------|------|------|
| 0:00 | "This is how I launch 16 GPUs today" | Distributed sbatch script. `torchrun`. Manual NCCL and rendezvous. |
| 0:45 | "This is Fuse on the same allocation" | `fuse status`. `fuse fabric`. Real 16-GPU slice, not a fake cluster. |
| 1:15 | Mental model | "Cluster → fabric → teams → jobs → models → checkpoints. Six primitives." |
| 1:45 | Auto-sharding | `fuse shard --model llama-70b --gpus 16`. Topology-aware TP/PP/DP on the live slice. |
| 2:30 | Real Fuse-managed job | `fuse train --example makemore`. Then `fuse jobs`, `fuse why`, `fuse logs`. |
| 3:15 | Real multi-GPU Fuse path | `fuse train --example nanochat --gpus 2 --steps 10 --hold 60`, then `fuse cancel`. |
| 4:00 | Real distributed boundary | Manual `2`-node `nanochat` Slurm launcher with the staged NGC image. |
| 4:45 | Why + agent-safe control | `fuse jobs --json`, `fuse why <job> --json`. Safe retries, explicit next actions, shared principal story. |
| 5:30 | Simulate beyond the slice | `fuse simulate --kill-node n1` or `fuse simulate --add-nodes 4 --switch leaf-01`. |
| 6:15 | Boundary line | "Everything up to here used the real cluster path. Simulation starts where the live slice ends." |
| 6:45 | Close | "Same Slurm cluster, better AI-native orchestration surface. Real 16-GPU reasoning, real jobs, and a safer control path for humans and agents." |

---

## Fuse vs Slurm vs Kubernetes

| | Slurm | K8s + Volcano | Fuse |
|---|---|---|---|
| **Train** | 40-line distributed sbatch | Helm chart + CRD | `fuse train --example nanochat` |
| **Serve** | sbatch + systemd | Deployment + Service + HPA | `fuse serve --model X` |
| **Topology** | `--switches` (broken) | None | Fabric: bandwidth-weighted graph |
| **Sharding** | Researcher guesses | Researcher guesses | Auto: TP/PP/DP from fabric + model |
| **NCCL** | Manual exports | Manual env in pod spec | Auto from PCIe affinity |
| **GPU sharing** | Partition limits | Resource quotas | Burst + checkpoint preemption |
| **Fair-share UX** | sprio/sshare (cryptic) | N/A | `fuse teams` (clear table) |
| **Failures** | Admin requeues manually | Pod restart (no ckpt) | Auto ckpt recovery, 30s |
| **Health** | None (use DCGM separately) | None (use DCGM separately) | `fuse doctor` (ECC, temp, NCCL) |
| **Capacity planning** | Spreadsheet | Spreadsheet | `fuse simulate` |
| **Explain decisions** | `(Priority)` | `Pending: 0/1 nodes available` | `fuse why` with suggestions |
| **Structured output** | No (flat text) | YAML (verbose) | `--json` on everything |
| **Idempotent** | No (sbatch twice = 2 jobs) | Yes (apply) | Yes (submit twice = same job) |
| **Agent friendliness** | Caller-managed, race-prone text CLI | API-level concurrency, pod-centric | Structured state, safer retries, explicit next actions, lower collision risk |
| **Models** | Not a concept | Not a concept | First-class with requirements |
| **Checkpoints** | Not a concept | Not a concept | First-class, tracked, verified |
| **Cost tracking** | sacct (wall time only) | N/A | GPU-hours, efficiency, bubble waste |
| **Setup** | 5+ config files | Helm + CRDs + operators | Single binary |

---

## Demo And Visual Assets

| File | What |
|------|------|
| `DEMO.md` | Honest run-of-show for the current live demo |
| `SLIDE.md` | Short deck source |
| `PRESENTATION.md` | Longer messaging and Q&A planning doc |
| `VISUALS.md` | Mermaid-ready visuals for Fuse vs Slurm, topology, Volcano, and multi-agent control |
