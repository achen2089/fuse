# Fuse Demo Visuals

This file contains the visual assets to support the current honest demo story.

These visuals are designed to help with three things:

1. Make the `Fuse vs Slurm` contrast obvious
2. Make the `16 GPU` cross-node topology story concrete
3. Make the `many agents, one cluster principal` wedge easy to understand

Use these in slides, the web app, or as source material for a polished deck.

## Visual 1: Same Cluster, Different Surface

Use this as the opening comparison.

```mermaid
flowchart LR
  A["Intent: train on the real cluster"] --> B1["Raw Slurm path"]
  A --> B2["Fuse path"]

  B1 --> C1["Write sbatch or srun script"]
  C1 --> D1["Pick image and mounts manually"]
  D1 --> E1["Set rendezvous and NCCL env manually"]
  E1 --> F1["Submit job"]
  F1 --> G1["Read text outputs from squeue/sacct"]
  G1 --> H1["Debug by SSH + logs + trial and error"]

  B2 --> C2["fuse shard"]
  C2 --> D2["Hardware-aware plan"]
  D2 --> E2["fuse train or fuse run"]
  E2 --> F2["fuse jobs / fuse why / fuse logs"]
  F2 --> G2["Structured JSON and safer retries"]
  G2 --> H2["Lower-friction control for humans and agents"]
```

What to say:

- Same cluster
- Same Slurm substrate
- Different operating surface

## Visual 2: The 16-GPU Slice

Use this when explaining why topology matters.

```mermaid
flowchart LR
  subgraph N1["Node A: 8 x B200"]
    A1["GPU 0-3"] --- A2["GPU 4-7"]
    A3["NVLink / NV18 island"]
  end

  subgraph N2["Node B: 8 x B200"]
    B1["GPU 0-3"] --- B2["GPU 4-7"]
    B3["NVLink / NV18 island"]
  end

  N1 <-->|"cross-node traffic\nsocket / IB locality matters"| N2
```

What to say:

- The allocation is real, not synthetic
- Cross-node is guaranteed
- Once the run crosses nodes, placement and sharding affect throughput directly

## Visual 3: Weighted Topology, Not Flat GPU Counts

Use this to sharpen the Fuse vs Slurm point.

```mermaid
flowchart TD
  A["Slurm view"] --> B["gpu: 16"]
  C["Fuse view"] --> D["2 nodes"]
  D --> E["8 GPUs per node"]
  E --> F["NVLink within node"]
  F --> G["NUMA and NIC locality"]
  G --> H["cross-node transport cost"]
  H --> I["better sharding plan"]
```

What to say:

- Slurm gives you a count
- Fuse gives you the shape

## Visual 4: Many Agents, One Cluster Principal

Use this as the core multi-agent visual.

```mermaid
flowchart LR
  U1["Operator"] --> F["Fuse control layer"]
  U2["Agent A"] --> F
  U3["Agent B"] --> F
  U4["Script / CI"] --> F

  F --> G1["actor IDs"]
  F --> G2["request IDs"]
  F --> G3["idempotent retries"]
  F --> G4["explicit next actions"]
  F --> G5["shared state and reasoning"]

  G1 --> H["shared cluster principal: user44"]
  G2 --> H
  G3 --> H
  G4 --> H
  G5 --> H

  H --> I["Slurm: sbatch / squeue / sacct / scancel"]
```

What to say:

- `user44` is transport, not real identity
- The wedge is not “an agent can call the CLI”
- The wedge is safe parallelism with lower collision risk

## Visual 5: Why Fuse, Not Volcano

Use this after the product story is already established.

```mermaid
flowchart LR
  A["Kubernetes + Volcano"] --> B["pods"]
  B --> C["CRDs"]
  C --> D["controllers"]
  D --> E["GPU jobs through K8s abstractions"]

  F["Fuse"] --> G["cluster"]
  G --> H["fabric"]
  H --> I["jobs"]
  I --> J["models"]
  J --> K["checkpoints"]
  K --> L["AI-native control over real Slurm"]
```

What to say:

- Volcano is the strongest Kubernetes-native comparison
- It still lives at the Kubernetes abstraction layer
- Fuse is trying to expose AI work directly with a thinner setup path

## Visual 6: Demo Run-Of-Show

Use this as your speaker map.

```mermaid
flowchart LR
  A["1. Real 16-GPU slice"] --> B["2. Show topology + shard plan"]
  B --> C["3. Real Fuse-managed job"]
  C --> D["4. Real multi-GPU single-node path"]
  D --> E["5. Honest distributed boundary"]
  E --> F["6. Agent-safe JSON surface"]
  F --> G["7. Simulation for scale-out"]
```

What to say:

- Live proof first
- Simulation second
- Honest boundaries throughout

## Visual 7: Fuse vs Slurm Operator Experience

Use this as a table slide.

| Task | Raw Slurm | Fuse |
|---|---|---|
| Understand the slice | `scontrol`, `sinfo`, manual topology probing | `fuse status`, `fuse nodes`, `fuse fabric`, `fuse topo` |
| Plan a 16-GPU run | human guesses | `fuse shard --model llama-70b --gpus 16` |
| Submit a simple training job | batch script or `srun` | `fuse train --example makemore` |
| Explain state | text from `squeue` / `sacct` | `fuse jobs`, `fuse why` |
| Logs | hunt down output path | `fuse logs <job>` |
| Safer automation | caller-managed | `--json`, explicit reasoning, lower-collision workflows |

## Visual 8: What Is Live vs What Is Direction

Use this to stay credible with technical audiences.

| Area | Live now | Direction / next step |
|---|---|---|
| Topology discovery | Yes | richer benchmarking |
| Sharding plans | Yes | tighter calibration |
| Single-GPU Fuse train | Yes | broader examples |
| Single-node multi-GPU `nanochat` | Yes | more validated images |
| Multi-node `nanochat` through Fuse | No | current gap |
| Multi-node manual Slurm launcher | Yes | bridge to full Fuse launcher |
| JSON / `why` surface | Yes | stronger action preconditions |
| Full multi-agent lease system | No | active design direction |

## Visual Style Notes

If you hand this off to a slide tool or another model:

- Keep the look terminal-first, not corporate
- Use dark neutrals, green/cyan accents, and one warning color
- Favor lane diagrams, graph diagrams, and operator tables over generic icons
- Make the `Fuse vs Slurm` contrast feel immediate
- Make the `many agents, one cluster principal` visual feel like a systems product wedge, not an AI gimmick
