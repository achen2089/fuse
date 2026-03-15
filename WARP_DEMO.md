# Warp Demo

This is the shortest credible live demo for Fuse.

Use this when you have about `90 seconds` and need the product wedge to land quickly.

## Thesis

Fuse is an AI-native orchestrator over Slurm: better for humans, usable by agents, and aware of real GPU topology.

## Opening Snippet

Use this before you type the first command:

```text
Topology-aware orchestration layer for distributed AI training. Fuse reads your NVLink domains, reasons about bandwidth tiers, auto-shards TP/PP/DP to fit your fabric, and replaces 40-line sbatch scripts with one command.

fuse nodes shows per-GPU health across your cluster. fuse fabric renders the switch topology with bandwidth tiers. fuse topo dumps the real NVLink matrix. fuse shard recommends parallelism strategies from measured hardware. fuse why explains scheduling decisions instead of printing (Priority).

Fair-share scheduling, checkpoint-first recovery, and agent-safe parallel control, all in a single binary on top of Slurm. Slurm sees "8 nodes with 8 GPUs each." Fuse sees the weighted graph.
```

## Open With One Visual

Before typing anything, put up:

- [VISUALS.md](/Users/anthonychen/Code/fuse/VISUALS.md) Visual 1: `Same Cluster, Different Surface`

Say:

`Same cluster, same Slurm underneath, but a much better operating surface on top.`

If you want a second visual at the end, use:

- [VISUALS.md](/Users/anthonychen/Code/fuse/VISUALS.md) Visual 4: `Many Agents, One Cluster Principal`

## Exact Command Sequence

Run these commands in order:

```bash
fuse status
fuse shard --model llama-70b --gpus 16 --nodes 2
fuse train --example makemore --name warp-demo --steps 200
fuse doctor warp-demo --json
```

## Talk Track

### 1. Prove this is a real cluster

Command:

```bash
fuse status
```

Say:

`This is a real Slurm-backed cluster, not a fake local simulator.`

`Fuse is operating over the actual cluster path, but it gives us a cleaner interface than raw Slurm commands and batch scripts.`

### 2. Show why topology matters

Command:

```bash
fuse shard --model llama-70b --gpus 16 --nodes 2
```

Say:

`We have a 16-GPU allocation, so cross-node is guaranteed.`

`At that point, the problem is not just counting GPUs. The orchestrator has to reason about the actual hardware shape and produce a sane sharding plan.`

`This is the difference between a scheduler that sees gpu:16 and one that understands the workload.`

### 3. Show the easier launch path

Command:

```bash
fuse train --example makemore --name warp-demo --steps 200
```

Say:

`Instead of writing a Slurm script, picking mounts, and wiring cluster details by hand, I can launch a real training job with one command.`

`Fuse is not replacing Slurm here. It is making Slurm usable.`

### 4. Close on humans plus agents

Command:

```bash
fuse doctor warp-demo --json
```

Say:

`The same surface works for both humans and agents.`

`Humans get a clear diagnosis, and agents get structured JSON instead of scraping unstable terminal text.`

`That is the wedge: same cluster, less operational overhead, and a control surface that works for parallel automation as well as a person at the terminal.`

## 90-Second Spoken Script

```text
GPU clusters are still operated through tools that were not built for AI workloads. Slurm can run the jobs, but the interface is still scripts, flags, and cluster folklore. Fuse sits on top of real Slurm and gives you a much better operating surface.

This is a real cluster, not a simulator. And because we have a 16-GPU allocation, cross-node is guaranteed. That means the hard part is no longer just counting GPUs. The system has to understand topology and produce a sane sharding plan for the hardware it is actually running on.

Then instead of writing a batch script, I can launch a real training job with one command. And once that job exists, Fuse gives me a structured diagnostic surface that works for both humans and agents. That is the core idea: same Slurm underneath, but a much more native orchestration layer on top.
```

## Safe Fallback

If the job has not shown up yet when you run `doctor`, use:

```bash
fuse jobs --json
```

Say:

`The important point is that the state is structured and easy to consume, whether the caller is a human, a script, or another agent.`

## What Not To Say

Do not say:

- `Fuse replaces Slurm today`
- `Fuse automatically shards arbitrary model code`
- `This is fully automatic multi-node training through Fuse today`
- `This already has a full multi-agent lease system`

Say instead:

- `Fuse is an orchestrator over Slurm`
- `Fuse makes sharding easier with hardware-aware plans`
- `Fuse is closing the gap between raw Slurm and AI-native orchestration`
- `Fuse gives humans and agents a better shared control surface`
