---
# Fuse
## AI-native orchestrator for GPU clusters, humans, and concurrent agents

Topology-aware training, agent-safe parallel control, and lower operational overhead over real Slurm.

Speaker note:
The honest MVP story is not "we replaced the cluster." The story is that Fuse adds an AI-native orchestration layer over Slurm that is easier for both humans and agents to operate.

---
# AI Work Has Outgrown Human-Only Cluster Tooling

- Slurm was built for CPU batch jobs
- Kubernetes was built for stateless services
- Modern AI work is cross-node, iterative, and increasingly agent-assisted
- Current tools expose infrastructure details instead of AI workload intent

Speaker note:
The problem is no longer just human inconvenience. AI teams now want both people and agents to drive the cluster in parallel, and the old abstractions are a bad fit for that.

---
# Fuse Adds An Agent-Friendly Control Layer Over Slurm

- Real execution still goes through `sbatch`, `squeue`, `sacct`, and `scancel`
- Fuse adds opinionated CLI workflows, reasoning, state, and simulation
- Structured `--json`, idempotent retries, and explicit next actions make automation safer
- Designed for low-collision workflows between operators and concurrent agents

Speaker note:
This is the key change from the earlier story. Today, Fuse is an AI-native orchestration layer over the cluster you already have, not a brand-new control plane you must install first.

---
# Many Agents, One Cluster Principal

- On this cluster, multiple agents can operate through the same `user44` credential
- Shared credential is transport, not the real identity model
- Fuse adds actor IDs, request IDs, safer retries, and action preconditions above Slurm
- The win is safe parallelism with lower collision risk
- Safe parallelism means more of the scheduler can be used without waiting on one controller

Speaker note:
The bar is not that an agent can shell out to `sbatch`. The bar is that multiple agents can use the same scheduler surface in parallel without duplicating work or stepping on each other.

---
# 16 GPUs Makes The Parallel Problem Real

- Our live setup is a real `16 GPU` slice on `2` B200 nodes
- Cross-node execution is guaranteed
- `NVLink` dominates inside a node, while IB locality matters across nodes
- Placement, rank assignment, and sharding plans now affect throughput directly

Speaker note:
This is not a toy single-node demo. The live slice forces the real parallel-training problem, which is exactly where topology-aware orchestration matters.

---
# Better DX For Humans And Agents

- One command to train `makemore` or `nanochat`, or diagnose
- Hardware-aware sharding plans instead of parallelism guesswork
- Container-first execution on a thin cluster image
- Read-only TUI plus structured outputs for terminals, scripts, and agents
- Less cluster folklore, less YAML, and less manual runbook work

Speaker note:
The product value is not just scheduling quality. It is making the common AI workflow easier to drive, easier to debug, and easier to automate safely.

---
# Why Not Volcano Or Existing Tools?

- Existing tools solve adjacent problems at the wrong abstraction layer
- Slurm counts resources, and Kubernetes plus Volcano manage pods, CRDs, and controllers
- They can run AI workloads, but they do not provide an AI-native, agent-friendly control surface
- Volcano also requires a much heavier setup path before the first real training job runs
- Fuse works with the real cluster path today and is easier to get to first job

Speaker note:
Volcano is probably the strongest comparison, but it is still a Kubernetes-native answer. Fuse is trying to make real GPU clusters easier for humans and agents to operate directly, with less platform machinery in the middle.
