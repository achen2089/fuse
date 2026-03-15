# Fuse

**GPU-native job scheduler — topology-aware training, auto-sharding, and fair-share scheduling in a single binary.**

---

## The story

Every GPU scheduler today was built for a world before large-scale AI training. Slurm was built for HPC batch jobs in the 90s. Kubernetes was built for stateless web services in the 2010s. Both treat GPUs as afterthoughts — opaque resource counters bolted on through plugins and CRDs.

Fuse starts from a different question: **what would a job scheduler look like if GPUs, training runs, and model-aware scheduling were the design center — not an addon?**

The answer has six primitives.

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

Not just a dev tool. It's a capacity planner, a training ground, and a demo engine.

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

## Designed for humans and machines alike

Not a protocol. Not a plugin. Just design decisions that make every interaction structured, explained, and predictable.

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

We're on a real Slurm cluster as `user44` (QOS `restricted_limit`, 1 GPU, 200G). The demo runs both schedulers on the same allocation.

```bash
# Get resources from Slurm (the normal way)
salloc --gres=gpu:1 --mem=200G --cpus-per-task=20 --time=04:00:00

# Start Fuse on the allocated node
fuse server --faker --port 9090 &
fuse agent --server localhost:9090 --discover nvidia
# 5 seconds. 65 GPUs (64 fake + 1 real). Full topology.
```

### 28 lines vs 1 line

```bash
# ═══ SLURM ═══                          # ═══ FUSE ═══

cat << 'EOF' > finetune.sbatch           fuse finetune \
#!/bin/bash                                --model llama-7b \
#SBATCH --job-name=llama-7b-lora           --method lora \
#SBATCH --gres=gpu:1                       --data ./alpaca.json \
#SBATCH --mem=50G                          --gpus 1
#SBATCH --cpus-per-task=4
#SBATCH --time=01:00:00                  # That's it.
#SBATCH --output=logs/llama-ft-%j.out    # Logs stream automatically.
#SBATCH --error=logs/llama-ft-%j.err     # Checkpoint auto-configured.
                                         # NCCL env auto-detected.
module load cuda/12.2                    # Failure → auto-recovery.
module load nccl
source activate train

export CUDA_VISIBLE_DEVICES=0
export NCCL_DEBUG=INFO
export NCCL_IB_DISABLE=1
export NCCL_SOCKET_IFNAME=eth0
export OMP_NUM_THREADS=4
export TOKENIZERS_PARALLELISM=false

python finetune.py \
  --model_name meta-llama/Llama-2-7b-hf \
  --dataset ./alpaca.json \
  --output_dir ./checkpoints \
  --per_device_batch_size 4 \
  --learning_rate 2e-4 \
  --num_train_epochs 1 \
  --lora_r 16 \
  --lora_alpha 32 \
  --bf16
EOF
mkdir -p logs
sbatch finetune.sbatch
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

### Multi-node NCCL (if 2 nodes approved)

```bash
# Slurm: 40 lines of sbatch with manual NCCL flags
# MASTER_ADDR=$(scontrol show hostnames ... | head -n 1)  ← incantation
# export NCCL_IB_HCA=mlx5_0:1    ← which HCA? run ibstat
# export NCCL_NET_GDR_LEVEL=5    ← GPUDirect? read docs
# export NCCL_SOCKET_IFNAME=ib0  ← which interface? guess
# (wrong value = silent 10× slowdown, no warning)

# Fuse:
fuse train --model llama-7b --nodes 2
# NCCL auto-configured from PCIe affinity. Warns if fallback to TCP.
```

---

## What the 1 real GPU does

Not proof-of-life. Real work.

**Real fine-tune.** LoRA fine-tune of Llama-7B. Full pipeline: NVML discovery → sharding → dispatch → env injection → training with loss curves → auto-checkpoint → completion. ~10 min. Crowd watches real loss go down.

**Real inference.** `fuse serve` loads the model. Health endpoint, request handling. Send prompts, get completions. Kill the process — watch auto-restart.

**Real benchmarks.** `fuse bench` profiles the GPU: BF16 TFLOPS, memory bandwidth. Numbers feed into the sharding recommender. "Fuse measured YOUR hardware."

**Real eval sweep.** `fuse eval --sweep` queues 5 evals, runs back-to-back, collects results.

**Hybrid cluster.** Real GPU joins the faker as node-09. Scheduler treats it identically. Real node shows live metrics, fakes show simulated data. Same dashboard, same API.

---

## Auto-sharding

```bash
fuse shard --model llama-70b --nodes 2

# Recommended: TP=8 PP=2 DP=1
#   TP=8 within node → NVLink 900 GB/s (64 heads ÷ 8 = 8 heads/GPU)
#   PP=2 across nodes → IB 50 GB/s (80 layers ÷ 2 = 40 layers/stage)
#   Memory/GPU: 43.2 / 80 GB (54%)
#   Bubble: 50%
#
# Alternative: TP=8 PP=1 DP=2 (no bubble, 71% util, grad sync over IB)
#
# Bench data: node-09 measured at 312 BF16 TFLOPS, 1.8 TB/s mem BW

fuse shard --model llama-405b --nodes 8
# → TP=8 PP=4 DP=2 (810 GB model, needs 4+ PP stages)
```

Logic: memory budget → TP sized to NVLink domain and attention heads → PP minimized for bubble → DP fills remaining → bandwidth cost per tier → hardware calibration from `fuse bench`.

---

## The TUI

```
╭─ fuse ──────────────────────────────────────────────╮
│  9 nodes  65 GPUs  49 alloc  16 idle  0 dead        │
│  6 running  3 pending  1 serving                     │
╰─────────────────────────────────────────────────────╯

  n1  [████████]  8/8  llama-train (ml-research)     47°C
  n2  [████████]  8/8  llama-train (ml-research)     45°C
  n3  [████░░░░]  4/8  eval-mmlu (nlp)               38°C
  n4  [░░░░░░░░]  0/8  idle
  n5  [████████]  8/8  vit-pretrain (vision)          52°C
  n6  [████████]  8/8  bert-ft (nlp) [burst]          49°C
  n7  [████████]  8/8  bert-ft (nlp) [burst]          48°C
  n8  [░░░░░░░░]  0/8  idle
  n9  [█░░░░░░░]  1/1  llama-7b-serve ★ REAL          61°C

  14:23  → llama-train: n1,n2 (same_switch, TP=8 PP=2 auto-sharded)
  14:38  ↗ bert-ft: burst on n6,n7 (ml-research idle capacity)
  14:50  ★ bench n9: 312 BF16 TFLOPS, 1.8 TB/s
  15:02  ⚡ bert-ft preempted → ckpt step-4800 → requeued
  15:07  ✗ n9 GPU crash → llama-7b-serve restarted (10s)
```

---

## The full CLI

### Workload verbs

```bash
fuse train     --model llama-70b --nodes 2 --data /pile
fuse finetune  --model llama-7b --method lora --data ./alpaca.json --gpus 1
fuse serve     --model llama-7b --gpus 1 --engine vllm --port 8000
fuse eval      --model llama-7b --gpus 1 --sweep --param benchmark=mmlu,arc,hellaswag
fuse bench     --type compute     # or: nccl, memory, health
fuse run       --gpus 1           # interactive shell with GPU
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
fuse shard     --model llama-70b --nodes 2     # sharding recommender
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
fuse kill-node <node>           # simulate failure (faker)
fuse team create <n> --quota N  # new team
fuse model register <n> --params P --layers L --heads H  # custom model
```

---

## What to build

```
fuse
├── cmd/
│   ├── server.go         # control plane
│   ├── agent.go          # node agent
│   └── cli.go            # all commands
├── pkg/
│   ├── scheduler/        # gang + bin-packing + topology
│   ├── fabric/           # switch graph, bandwidth tiers
│   ├── discovery/        # faker + nvidia (NVML)
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
| 0–1 | Server + faker + `status`/`fabric`/`nodes` | `fuse server --faker` boots on salloc node |
| 1–3 | Scheduler + job lifecycle + `fuse train` | Job → topology placement → RUNNING → COMPLETED |
| 3–4 | `fuse shard` + `fuse bench` on real GPU | Shard recommends. Bench reports real TFLOPS. |
| 4–5 | Real fine-tune + `fuse serve` on real GPU | LoRA runs, model served, prompts answered |
| 5–6 | `fuse doctor` + `fuse why` + Slurm comparison | Diagnostics work. Side-by-side sbatch vs fuse. |
| 6–7 | Faker simulate + fault injection + rehearsal | `fuse simulate --kill`. Practice 7-min demo. |

### Cut list (drop from bottom)

1. Server + faker + CLI — **must ship**
2. Scheduler with fabric awareness — **must ship**
3. Job state machine with types — **must ship**
4. `fuse bench` on real GPU
5. `fuse shard` + model registry
6. `fuse train` / `fuse finetune` on real GPU
7. `fuse doctor` + `fuse why`
8. `fuse serve` on real GPU
9. TUI (React artifacts as fallback)
10. `fuse simulate` (dry-run failures, capacity planning)
11. `fuse cost`
12. Slurm side-by-side
13. `fuse snapshot --json`
14. Checkpoint tracking + GC

---

## Demo script (7 min)

| Time | Beat | Show |
|------|------|------|
| 0:00 | "This is how I train today" | sbatch script. 28 lines. squeue. sprio gibberish. |
| 0:45 | "This is Fuse" | `fuse server --faker`. 5 sec. `fuse status`. `fuse fabric`. TUI. |
| 1:15 | Mental model | "Cluster → fabric → teams → jobs → models → checkpoints. Six primitives." |
| 1:45 | One-liner fine-tune | `fuse finetune --model llama-7b --method lora --gpus 1`. Real loss curve. |
| 2:30 | Auto-sharding | `fuse shard --model llama-70b --nodes 2`. Topology-aware TP/PP/DP. |
| 3:15 | Doctor | `fuse doctor cluster`. Health, ECC, congestion, silent NCCL fallback. |
| 4:00 | Why | `fuse why abc123 pending`. Explained decision with suggestions. |
| 4:30 | Fair-share | `fuse teams`. Burst. Preemption. Compare to sshare. |
| 5:15 | Simulate | `fuse simulate --kill-rack rack-01`. Dry-run failure. Capacity planning. |
| 5:45 | Real serve | `fuse serve --model llama-7b`. Prompt → completion. Kill → auto-restart. |
| 6:15 | Fault recovery | Kill fake node in TUI. 30-second cascade. |
| 6:45 | Close | "Same Slurm cluster. 28 lines vs 1. Manual NCCL vs auto. Cryptic errors vs fuse doctor. Debugging failures vs 30-second recovery. Six primitives that make sense." |

---

## Fuse vs Slurm vs Kubernetes

| | Slurm | K8s + Volcano | Fuse |
|---|---|---|---|
| **Fine-tune** | 28-line sbatch | Helm chart + CRD | `fuse finetune --model X` |
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
| **Models** | Not a concept | Not a concept | First-class with requirements |
| **Checkpoints** | Not a concept | Not a concept | First-class, tracked, verified |
| **Cost tracking** | sacct (wall time only) | N/A | GPU-hours, efficiency, bubble waste |
| **Setup** | 5+ config files | Helm + CRDs + operators | Single binary |

---

## Artifacts already built

| File | What |
|------|------|
| `fusion-tui.jsx` | TUI: status/nodes/jobs/topo/shard, GPU heatmap, terminal, events |
| `fair-share-demo.jsx` | Interactive Fuse vs Slurm fair-share with utilization comparison |
| `auto-sharding.jsx` | Model parallelism recommender, strategy ranking, YAML generation |