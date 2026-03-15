# Fuse Presentation Plan

This document is for preparing the story before the final demo or deck exists. It is the working outline for what we are building, how we want to explain it, and what must be ready before presenting.

## Presentation Goal

Explain Fuse as a GPU-native orchestrator and agent-safe control layer for AI workloads, built over the real cluster path instead of retrofitted from HPC or web infrastructure abstractions, and show why that matters in practice.

## One-Sentence Pitch

Fuse is a GPU-native orchestrator for AI workloads and agentic operations: it understands topology, models, checkpoints, and team-level fairness, and lets many agents operate the same cluster safely even when they share one cluster principal.

## Coherent Messaging

Keep the story disciplined. The core message stack is:

1. GPU clusters today are managed by tools that do not understand GPU work.
2. Our `16 GPU` allocation guarantees a cross-node run, so topology is immediately part of the problem.
3. Fuse uses AI-native primitives: `cluster`, `fabric`, `teams`, `jobs`, `models`, and `checkpoints`.
4. That lets Fuse improve scheduling, developer experience, and operations at the same time.
5. Fuse is also a better surface for concurrent agents because it is structured, legible, and lower-collision by design, even when multiple automations share the same cluster credential.
6. Volcano is a strong Kubernetes-native scheduler, but it still operates on Kubernetes primitives. Fuse is trying to operate on AI workload primitives.

## Product Experience

Fuse should not only be technically better. It should feel better to use.

The product stance should be opinionated:

- Nice CLI and UI sugar over repetitive cluster tasks
- Conventions for training, serving, checkpointing, and diagnosis
- Defaults that remove infrastructure ceremony from common ML workflows
- Easy sharding through topology-aware sharding plans and launcher generation
- Structured output for people, scripts, and AI agents sharing the same cluster principal
- Agent-safe control surfaces with safer retries and clearer next actions
- A terminal-first TUI for understanding cluster state, fabric, jobs, checkpoints, and failures
- Escape hatches for advanced users, without forcing everyone into low-level configuration first

The message here is important: conventions are part of the value. The goal is not maximum surface area. The goal is to make the common path fast, legible, hard to misuse, and safer for concurrent automation.

## Main Narrative

GPU clusters today are managed by tools that do not understand GPUs.

Slurm was built for CPU batch jobs in the 1990s. It sees GPUs as numbers in a config file, not as devices connected by NVLink at 900 GB/s or InfiniBand at 400 Gbps. Kubernetes was built for stateless web services. It adds GPU support through operators and CRDs that turn basic scheduling into platform plumbing.

The result is the same everywhere:

- Researchers copy NCCL settings from wiki pages
- Parallelism choices are guessed through trial and error
- Jobs wait in opaque queues while GPUs sit idle behind policy boundaries
- Hardware failures are debugged manually by SSHing into nodes
- A simple training run turns into a long batch script full of infrastructure-specific incantations

At scale, failure is not rare. Something breaks every few hours, and the recovery path is usually a manual runbook that starts with paging an admin.

Our setup makes that mismatch concrete. We have a real `16 GPU` allocation, which guarantees a cross-node run. This is not a toy single-node case. Once a job spans nodes, the scheduler has to reason about `NVLink` within a node, `InfiniBand` across nodes, placement quality, communication cost, and how work should be split. A system that only sees "16 GPUs" is operating at the wrong abstraction level.

Fuse starts from a different premise: the orchestration layer should understand the work.

Fuse introduces six primitives that map to how ML teams actually think:

- Cluster
- Fabric
- Teams
- Jobs
- Models
- Checkpoints

That changes the operator and researcher experience:

- One command to train
- One command to serve
- One command to diagnose
- Topology-aware placement that understands NVLink, PCIe, and InfiniBand
- Easy sharding through hardware-aware parallelism plans
- Fair-share that fills idle GPUs instead of wasting them behind partitions
- Checkpoint-aware recovery that reschedules quickly instead of waiting on a human
- Agent-safe operations with less collision risk between concurrent automations
- AI-native operations with clear diagnostics and a usable terminal-first interface

For the current MVP, Fuse is an AI-native orchestration layer over Slurm, not a replacement control plane on the live cluster. The value is that it gives GPU clusters a more native, more legible, and more agent-friendly operating surface today.

## High-Level Script

```text
GPU clusters today are managed by tools that do not understand GPU work. Slurm was built for CPU batch jobs, and Kubernetes was built for stateless services. Both can be adapted to AI workloads, but neither is built around the real shape of distributed training: topology, parallelism, checkpoints, and failure recovery.

Our setup makes that concrete. We have a 16-GPU allocation, which guarantees a cross-node run. Once a job spans nodes, the hard part is no longer counting GPUs. The scheduler has to reason about NVLink within a node, InfiniBand across nodes, placement quality, communication cost, and how the workload should be split. A system that only sees “16 GPUs” is operating at the wrong abstraction level.

Fuse starts from a different premise: the orchestration layer should understand the work. It treats cluster, fabric, teams, jobs, models, and checkpoints as first-class primitives. That lets it make topology-aware placement decisions, support hardware-aware sharding plans, share capacity more fairly, and recover from failures through checkpoints instead of manual runbooks.

Just as important, Fuse should feel better to use. The experience should be opinionated: one command to train, one to diagnose, sensible defaults, structured output, and a clear terminal-first UI or TUI for seeing what the cluster is doing.

It should also be safer for concurrent automation. The goal is not just that an AI agent can call a command. The goal is that multiple agents can inspect state, retry actions, and coordinate around the same cluster without colliding or duplicating work. On this cluster, that can mean multiple agents effectively acting through the same `user44` credential. Fuse should treat that shared credential as transport and layer actor identity, safe retries, and action preconditions above it. That is a real product difference, not just a nice property.

That is also the clearest distinction from Volcano. Volcano is a strong Kubernetes-native scheduler, but it still schedules pods and controllers. Fuse is trying to expose AI work directly through a thinner orchestration layer. Volcano schedules GPU-shaped pods. Fuse gives humans and agents a better surface for AI work.
```

## Safe Language

Use these phrases:

- `easy sharding`
- `hardware-aware sharding plans`
- `topology-aware parallelism defaults`
- `launcher or config generation for supported models and runtimes`
- `many agents, one cluster principal`
- `agent-safe scheduling`
- `safer retries and lower collision risk for concurrent agents`

Avoid broad claims like:

- `automatic sharding for any model`
- `fully automatic parallelism for arbitrary training code`
- `fully autonomous multi-agent control with no coordination`
- `shared credentials magically solve coordination`

## Message Hierarchy

When presenting, keep the message stack simple:

1. Current schedulers do not understand AI workloads or GPU topology.
2. Our `16 GPU` run guarantees cross-node behavior, so topology is a first-order concern.
3. That gap creates operational waste, scheduling friction, and manual recovery work.
4. Fuse closes that gap by making AI-native concepts first-class scheduling primitives.
5. Fuse adds opinionated conventions so users ask for work, not infrastructure ceremony.
6. Fuse is also shaped to be safer for humans and concurrent agents to operate together.
7. The result is better placement, better utilization, faster recovery, and safer automation.

## The Core Story

Most schedulers treat GPUs like generic counters.

Fuse treats the full AI system as first-class:

- The physical cluster
- The network fabric
- Team quotas and burst usage
- AI job types
- Model requirements
- Checkpoints and recovery

That is the difference between "submitting a job" and "running AI infrastructure intelligently."

## What We Are Actually Showing

The presentation should stay anchored on a small number of concrete claims:

1. Fuse understands hardware topology instead of pretending every GPU is equivalent.
2. A `16 GPU` allocation guarantees a real cross-node story, not a single-node demo.
3. Fuse reasons about AI jobs differently based on workload type.
4. Fuse makes checkpoints part of the scheduler, not just a file on disk.
5. Fuse supports fair sharing and burst usage across teams.
6. Fuse treats failure recovery as a normal operating path, not an admin-only emergency.
7. Fuse is intentionally opinionated in the user experience so common tasks require less YAML, fewer flags, and less cluster folklore.
8. Fuse exposes structured, retry-safe control surfaces that reduce collisions between concurrent agents.

## Current Live Scope

We now have a real 16-GPU allocation. The presentation should optimize for that advantage instead of centering the simulator.

Treat these as live proof points:

- Discover the real Slurm allocation and show topology
- Run a real distributed `nanochat` job across the slice
- Use `makemore` as the fastest bring-up and smoke path
- Show a hardware-aware sharding plan against the live hardware
- Show checkpoint-aware recovery after an induced failure
- Keep the live path Slurm-backed and daemon-free on the cluster
- If time permits, show the JSON / `why` / next-action surface as the agent-safe control plane
- Make clear that `user44` is the shared cluster credential and Fuse is the coordination layer above it

Treat these as simulator or backup material:

- 64+ GPU capacity planning
- Rack- or cluster-level failure scenarios
- Global fair-share behavior outside the 16-GPU slice
- Any `fuse server` behavior outside local `--faker` simulation

This keeps the talk stronger and more honest. The audience sees real distributed behavior first, then simulation where the live allocation stops.

Agent-friendliness should be framed as the second wedge in the story:

- First wedge: topology-aware AI scheduling on a real cross-node run
- Second wedge: safer, more legible control for humans and concurrent agents

## Short Deck Version

### Slide 1

Title:

**AI teams are running on the wrong foundation**

Points:

- GPU clusters are still managed by tools built for other eras
- Slurm was built for CPU batch jobs
- Kubernetes was built for stateless services
- AI workloads inherit complexity those systems were never designed to handle

Speaker line:

AI infrastructure is still being forced through abstractions that were not built for it.

### Slide 2

Title:

**Cross-node training changes the problem**

Points:

- Our live setup is a real `16 GPU` allocation
- That guarantees a cross-node run on this cluster
- `NVLink` matters within a node
- `InfiniBand` matters across nodes
- Placement and workload splitting now affect throughput directly

Speaker line:

Once a run spans nodes, the orchestrator cannot just count GPUs. It has to reason about the network and the workload.

### Slide 3

Title:

**Fuse is an AI-native orchestrator**

Points:

- Built around `cluster`, `fabric`, `teams`, `jobs`, `models`, and `checkpoints`
- Understands topology, workload shape, and recovery paths
- Makes sharding easier with hardware-aware plans
- Operates on AI work directly instead of generic infrastructure objects
- Exposes more agent-friendly control surfaces than legacy schedulers

Speaker line:

Fuse changes the abstraction layer from infrastructure objects to AI workload primitives, and that also makes the system easier for agents to operate safely.

### Slide 4

Title:

**More product sugar, less operational overhead**

Points:

- One command to train, serve, or diagnose
- Opinionated defaults instead of cluster folklore
- Easy sharding for supported runtimes
- Structured output and a usable terminal-first TUI
- Safer control surfaces for humans and concurrent agents
- More native abstraction, less YAML, and less control-plane overhead
- Faster time-to-first-job with a lighter setup path

Speaker line:

This is not just a smarter backend. It is a more native way to operate AI infrastructure, with less ceremony for developers, less overhead for operators, and a much easier path to getting the system running.

### Slide 5

Title:

**Agent-friendly control is a real wedge**

Points:

- Most schedulers are hostile to concurrent automation
- Text parsing, duplicate retries, and stale reads create collisions
- Fuse pushes toward JSON state, explicit next actions, safer retries, and actor identity above shared credentials
- The value is not "an agent can call the CLI"
- The value is "multiple agents can operate the same cluster in parallel with less risk"
- Safe parallelism is how the scheduler gets used more aggressively instead of becoming a single-agent bottleneck

Speaker line:

As more teams use agents for triage, launch, and recovery, the scheduler itself has to become a safer coordination surface.

### Slide 6

Title:

**Why not Volcano or existing tools?**

Points:

- Existing tools solve adjacent problems at the wrong abstraction layer
- Slurm counts resources, and Kubernetes plus Volcano manage pods, CRDs, and controllers
- They can run AI workloads, but they do not natively model topology, checkpoints, or AI workload behavior
- The result is more YAML, more glue code, and more operational overhead
- Volcano also requires a much heavier setup path before the first real training job runs
- Fuse is designed around AI workload primitives from the start

Speaker line:

Volcano is probably the strongest existing comparison, but it is still a Kubernetes-native answer. It schedules GPU-shaped pods. Fuse is trying to orchestrate AI work directly, with a thinner setup story and less platform machinery between the user and the GPU.

## Suggested Deck Structure

### 1. Title

**Fuse: an AI-native orchestrator for GPU clusters**

Subhead:

**Topology-aware scheduling, model-aware placement, and checkpoint-aware recovery in one system**

Talk track:

- We are not building a general-purpose cluster manager.
- We are building an orchestration layer specifically for AI workloads on GPU clusters.

### 2. The Problem

Title:

**Today’s schedulers were not designed for AI training**

Points:

- Slurm was built around batch HPC jobs
- Kubernetes was built around stateless services
- GPUs, network topology, and checkpoint recovery are treated as secondary concerns

Talk track:

- Large model training is sensitive to placement, bandwidth, and failure recovery.
- Existing systems can run AI jobs, but they do not reason about them natively.

### 3. The Insight

Title:

**AI scheduling needs different primitives**

Show the six Fuse primitives:

- Cluster
- Fabric
- Teams
- Jobs
- Models
- Checkpoints

Talk track:

- This is the conceptual center of the project.
- The scheduler should understand not just resources, but the structure of the workload.

### 4. 16 GPUs Changes The Problem

Title:

**Cross-node is guaranteed, so topology matters**

Points:

- We have a real `16 GPU` allocation
- On this cluster, `16 GPUs` guarantees a cross-node run
- `NVLink` matters within a node
- `InfiniBand` matters across nodes
- Placement and workload splitting now affect performance directly

Talk track:

- This is where the problem becomes real.
- Single-node demos hide the hard part. A `16 GPU` run forces the hard part.

### 5. What Makes Fuse Different

Title:

**Fuse schedules with more context**

Comparison:

- Slurm sees a node count
- Fuse sees topology and bandwidth tiers
- Slurm sees one generic job path
- Fuse distinguishes smoke, train, serve, and eval paths
- Slurm sees failure after the fact
- Fuse uses health and checkpoints to recover automatically
- Slurm exposes text and caller-managed coordination
- Fuse exposes structured, lower-collision control surfaces

Talk track:

- The value is not only better placement.
- The value is operational intelligence under real cluster conditions.

### 6. Example Workflow

Title:

**What an operator or researcher actually does**

Example flow:

1. Discover cluster topology
2. Register or select a model
3. Submit a training job
4. Let Fuse choose placement and generate a sharding plan
5. Track checkpoints and recover automatically on failure

Talk track:

- We want the user experience to feel simple.
- The complexity should live in the scheduler, not in hand-written job logic.

### 7. Opinionated UX

Title:

**Less cluster ceremony, more useful defaults**

Points:

- One command to train
- One command to serve
- One command to diagnose
- Sensible defaults for placement and parallelism
- Easy sharding for supported runtimes
- Structured output for both humans and automation
- Safer retries and explicit next actions for agents
- A usable terminal-first TUI

Talk track:

- A large part of the product value is reducing operational friction.
- Fuse should feel like a system that already knows the shape of AI work.

### 8. Safer For Agents

Title:

**Concurrent agents need safer control surfaces**

Points:

- Text CLIs create duplicate work and race conditions
- Retry should not mean duplicate submission
- Agents should see valid next actions, not guess state machines
- Shared credentials need logical actor identity above the transport layer
- Humans should be able to audit what automation did
- Safe parallelism is what turns agent usage into real scheduler utilization

Talk track:

- This is where "agent-friendly" becomes concrete.
- The win is not compatibility. The win is safe parallelism with lower collision risk.

### 9. Fairness And Utilization

Title:

**Better cluster sharing without idle waste**

Points:

- Teams get quotas
- Idle capacity can burst to other teams
- Burst jobs are preemptable
- Fair-share prevents one team from dominating the cluster over time

Talk track:

- This is important because GPU clusters are expensive and politically sensitive.
- A scheduler that improves utilization but ignores fairness will fail in real organizations.

### 10. Failure Recovery

Title:

**Failure is normal, so recovery must be automatic**

Points:

- Detect degraded or failed GPUs
- Use checkpoints as scheduler-managed recovery points
- Requeue and reschedule automatically
- Avoid wasting healthy GPUs on partially degraded nodes

Talk track:

- This is one of the strongest parts of the story.
- AI training is long-running, so recovery behavior matters almost as much as placement.

### 11. Demo Or Simulation

Title:

**What we want to show live**

Preferred live sequence:

1. Show the discovered 16-GPU allocation and topology
2. Show `fuse shard` or equivalent sharding-plan output on the live slice
3. Launch a real distributed `nanochat` training job
4. Run `fuse doctor` and induce a failure
5. Show checkpoint-aware recovery on the live slice
6. Optionally show `--json` / `fuse why` / explicit next actions as the agent-safe surface
7. Use `fuse simulate` only for >16-GPU or rack-level scenarios

Backup if live demo is risky:

- Record the real 16-GPU workflow end-to-end
- Keep fixed screenshots of topology, shard output, doctor output, and recovery
- Use simulator only after stating the live boundary clearly

Talk track:

- The demo should prove the architecture on real hardware first. Simulation comes after the live proof, not instead of it.
- Keep the workload simple. `makemore` is the quick validation path, `nanochat` is the live distributed path, and Axolotl or LlamaFactory are optional stretch examples only if there is extra time.

### 12. Why It Matters

Title:

**The payoff**

Points:

- Higher GPU utilization
- Better placement for distributed training
- Faster recovery from failures
- Less operator intervention
- Fairer multi-team cluster usage
- Safer automation as more cluster work shifts to agents

Talk track:

- The point is not that Fuse is "different."
- The point is that AI infrastructure behaves better when the scheduler understands AI.

### 13. Closing

Title:

**Build the scheduler AI clusters actually need**

Closing line:

Fuse turns GPUs, topology, models, and checkpoints into first-class scheduling primitives so AI teams can run faster and waste less infrastructure.

## Competitive Framing

When asked about alternatives, the framing should stay disciplined. Do not dismiss existing systems casually. Show respect for what they solve, then explain why Fuse uses a different abstraction level.

### Volcano, one-line answer

Volcano is a strong GPU scheduler built on Kubernetes primitives. Fuse is a GPU scheduler built on GPU primitives.

### Volcano, short answer

Volcano is probably the closest competitor because it takes gang scheduling and GPU workloads seriously. But it is still a Kubernetes answer to an AI scheduling problem. It schedules pods, quotas, CRDs, and controllers. Fuse schedules training runs, topology, models, checkpoints, and team behavior directly.

### Volcano, full answer

Volcano is the best answer the Kubernetes ecosystem has for GPU training, and that is exactly the limitation. It adds batch scheduling semantics onto a platform built for stateless services.

You get useful features such as:

- Gang scheduling
- Fair-share queues
- Job lifecycle management

But you get them through Kubernetes primitives:

- Pods
- Containers
- RBAC
- YAML
- Helm charts
- Controllers and operators

That means the system still describes infrastructure more naturally than it describes work.

The abstraction gap matters:

- Volcano does not understand NVLink versus PCIe versus InfiniBand as first-class scheduling inputs
- It does not know whether NCCL silently fell back to TCP
- It does not reason about model memory footprint or sharding plans
- It does not treat checkpoints as scheduler-managed recovery objects
- It does not distinguish simple validation runs from larger training shapes as different scheduling problems

Volcano can schedule pods that request `nvidia.com/gpu: 8`.

Fuse is trying to schedule training runs that need 64 GPUs with tensor parallelism inside NVLink domains and pipeline parallelism across InfiniBand links.

The difference is not just features. The difference is the level of abstraction.

### Volcano, operations angle

If someone pushes on operations, this is the other strong response:

Volcano requires a full Kubernetes control plane and ecosystem before you schedule a single job. That usually means:

- etcd
- API server
- controller manager
- container runtime
- CNI networking
- NVIDIA GPU operator
- monitoring stack such as DCGM and Prometheus

Fuse should be framed as a simpler, more opinionated system with a smaller operational footprint. In the ideal story, Fuse is a single binary or a much thinner control plane with fewer moving parts between the user and the GPU.

That makes the setup argument concrete:

- Faster time to first job
- Less platform setup before users see value
- Fewer systems to install, upgrade, and debug
- Lower operator burden for teams that do not want to run a full Kubernetes stack

### How To Say It Without Overreaching

- Respect Volcano as the strongest Kubernetes-native comparison
- Do not claim Kubernetes is useless for AI
- Focus on primitives, abstraction level, and operator burden
- Keep returning to the line: Volcano schedules GPU-shaped pods, Fuse schedules AI work

## Optional Appendix Slide

### Why Not Volcano?

Title:

**The closest competitor still lives at the wrong abstraction layer**

Points:

- Good answer inside Kubernetes
- Still centered on pods, CRDs, and controllers
- Does not make topology, models, or checkpoints first-class
- Higher operational footprint
- Fuse is designed around AI workload primitives from the start

## Short Verbal Version

If we only have 30 to 45 seconds:

Fuse is a GPU-native scheduler for AI clusters. Our `16 GPU` setup guarantees a real cross-node run, which means topology matters immediately. Instead of treating GPUs like generic resources, Fuse understands fabric, workload type, model constraints, team quotas, and checkpoints. That lets it place jobs more intelligently, make sharding easier, operate with better defaults, and recover from failures with less manual work.

## What Must Be Ready Before Presenting

These are the things we should prepare before claiming the story publicly.

### Must-have

- A clean one-line description of Fuse
- A simple architecture or mental-model diagram
- One real 16-GPU workflow example
- One coherent explanation of why `16 GPUs` forces a topology-aware story
- One fairness example
- One failure-recovery example on the live slice

### Strongly Recommended

- A terminal demo or recorded 16-GPU workflow
- Screenshots of the scheduler outputs
- A short comparison slide against Slurm and Kubernetes
- A tight explanation of why topology matters for distributed training
- A short explanation of why concurrent agents collide today and why Fuse is safer

If there is time for only one secondary differentiator beyond topology, make it agent-friendly control.

## Claims To Be Careful About

Avoid overstating anything that is not implemented yet.

Be precise about whether something is:

- Implemented
- Simulated
- Planned
- Mocked for demo purposes

If a feature is conceptual, present it as design direction, not shipped behavior. If a feature is only shown beyond the 16-GPU slice, say explicitly that it is simulated.

## Presenter Notes

- Keep the story concrete. Avoid abstract platform language.
- Lead with the problem, not the architecture diagram.
- Do not spend too long on every primitive. Use them to support the main thesis.
- If time is short, focus on topology, fairness, and recovery.
- The audience should leave understanding why AI scheduling needs different assumptions.

## Build-To-Presentation Checklist

Before the real presentation, confirm:

1. What exact shape the 16-GPU allocation has.
2. Which commands or screens are real on that slice today.
3. Which outputs are mocked, speculative, or simulated beyond the slice.
4. Which workflow we will demo live on the 16 GPUs.
5. Which workflow we will keep as backup screenshots or recording.
6. Who is speaking for each section.
