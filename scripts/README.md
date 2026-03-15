# Cluster Recon

This directory is the home for cluster-specific discovery, test, and capture commands while Fuse is being built against the live Slurm environment.

Use it for two things:

- keeping one reproducible way to probe the cluster
- recording the concrete facts we have already validated so the product story stays grounded in the real hardware

## Files

- `slurm-recon.sh`: run a lightweight or full probe either on a login node or over SSH
- `slurm-smoke.sh`: run the Fuse-specific smoke tests that matter for the Slurm adapter and live-demo path
- `fuse-smoke.sh`: use the local Fuse CLI to submit one real GPU container job, then verify `jobs`, `why`, and `logs` end-to-end

## Binary Convention

- The canonical repo-local binary is `./fuse` after `make build`.
- The installed binary is `fuse` after `make install`.
- `./.bin/fuse-live` is a generated compatibility wrapper for older scripts. It should not be treated as a second primary binary.

## Validated On 2026-03-15

The commands below were run from `user44@184.34.82.180`, which landed on `us-west-a2-login-005`.

### Fuse CLI smoke

We also validated the end-to-end Fuse-managed live smoke path through `scripts/fuse-smoke.sh`.

What happened:

- the local Fuse CLI submitted a real Slurm-backed GPU job as Slurm job `1535`
- the job ran on `us-west-a2-gpu-013`
- `fuse jobs`, `fuse why`, and `fuse logs` all worked against the same Fuse-managed job
- the public `pytorch/pytorch:2.7.0-cuda12.8-cudnn9-runtime` image succeeded on B200
- the workload fetched `karpathy/makemore` through the zip fallback path and printed the expected success marker

Operational takeaway:

- the local CLI over SSH path is real
- the current public PyTorch image is good enough for a live single-GPU smoke
- the new smoke script is a reproducible way to prove the core Fuse MVP path end-to-end

### Slurm shape

- Partitions currently exposed: `priority` (default), `unpreemptible`, and an empty `cpu` partition.
- GPU nodes: `us-west-a2-gpu-[001-032]`
- Node features: `nvidia_b200`, `nvidia_gpu`, `gpu_node`
- Each GPU node advertises:
  - `8` GPUs
  - `192` CPUs
  - `2043912 MiB` RAM, about `1.95 TiB`

Important correction:

- `two_node` is a `QOS`, not a partition.
- The earlier instinct to treat `two_node` like a partition was wrong.
- The actual partitions are `priority` and `unpreemptible`; `two_node` is the cap that limits what `user44` can request.

### User44 limits

`sacctmgr` reported the default QOS for `user44` as `two_node`.

`sacctmgr -nP show qos two_node format=Name%20,MaxTRESPU%100,MaxWall,Flags%30` returned:

```text
two_node|cpu=384,gres/gpu=16,mem=4000G||
```

That means the current practical ceiling for the Fuse POC is:

- `16` GPUs
- `384` CPUs
- `4000G` memory
- effectively `2` full B200 nodes

### GPU baseline

A 1-GPU probe landed on `us-west-a2-gpu-013` and reported:

- GPU model: `NVIDIA B200`
- GPU memory: `183359 MiB`
- driver version: `590.48.01`
- the sampled GPU sat on NUMA node `1`
- both visible InfiniBand NICs were reachable at `NODE` distance from that sampled GPU

### Full 8-GPU topology

A full-node probe landed on `us-west-a2-gpu-014`.

What the topology showed:

- every GPU pair inside the node is connected by `NV18`
- GPUs `0-3` are on NUMA node `0`
- GPUs `4-7` are on NUMA node `1`
- GPUs `4-5` are `PIX` to both visible IB NICs, which is the best NIC locality in the node
- GPUs `6-7` are `NODE` to both visible IB NICs
- GPUs `0-3` are `SYS` to both visible IB NICs, so cross-node traffic from those GPUs crosses the NUMA boundary first

This is the most important scheduler fact we learned:

- Slurm exposes `8` GPUs.
- Fuse can expose the weighted graph inside those `8` GPUs.
- For cross-node jobs, rank placement should prefer GPUs `4-5` for the NIC-nearest communication-heavy ranks.
- Tensor parallel should stay inside one node whenever possible because the intra-node fabric is dramatically better than crossing the host boundary.

### IB devices observed

The node exposed these InfiniBand devices:

- `ibp115s0f0`: active, `100 Gb/sec (2X HDR)`
- `ibp116s0f0`: active, `100 Gb/sec (2X HDR)`

The probe also showed additional `rdmap*` devices reporting `400 Gb/sec (8X HDR)`. We have not yet mapped how those devices show up in NCCL or whether they are the interfaces that matter for multi-node training. That is a follow-up item, not a confirmed scheduling fact yet.

### Live allocation smoke tests

We validated the real allocation path, not just the static limits:

- A `2`-node, `1`-GPU-per-node `srun` landed immediately on `us-west-a2-gpu-027` and `us-west-a2-gpu-015`.
- A full `2`-node, `8`-GPU-per-node `srun` also landed immediately on `us-west-a2-gpu-015` and `us-west-a2-gpu-008`.
- Each node in that full-slice smoke test reported exactly `8` visible B200s.

That matters because it means the Fuse live demo can reasonably assume the full `16`-GPU slice is practical, not merely allowed on paper.

### Runtime availability on the base image

On a sampled compute node:

- `python3` is present at `/usr/bin/python3`
- Python version is `3.10.12`
- `torchrun` was not in `PATH`
- `all_reduce_perf` was not in `PATH`
- `conda` was not in `PATH`
- importing `torch` with the stock Python failed

Operational takeaway:

- the base node image is intentionally thin
- Fuse should assume workloads bring their own environment or container rather than depending on site-wide ML packages

### Container support

The Slurm CLI exposes pyxis-style flags on both `srun` and `sbatch`, including:

- `--container-image`
- `--container-mounts`
- `--container-workdir`
- `--container-name`

Also validated:

- `enroot` exists at `/usr/bin/enroot`
- `docker`, `nerdctl`, `podman`, `apptainer`, and `singularity` were not in `PATH`

Operational takeaway:

- the cluster is already set up for a container-first workflow through Slurm
- for the Fuse POC, container launch is the most credible path for any real training or NCCL benchmark

### Slurm adapter smoke tests

We validated the exact submit/query/cancel loop that Fuse relies on:

- A short `sbatch --wrap` GPU job completed successfully and wrote the expected output file.
- A long-running `sbatch --wrap 'sleep 300'` GPU job was canceled with `scancel`.
- `sacct` reported the expected state transitions for both jobs.

Operational takeaway:

- the minimal Slurm control loop for Fuse is real on this cluster:
  - `sbatch`
  - `squeue`
  - `sacct`
  - `scancel`

### Container runtime smoke tests

We validated the real pyxis/enroot execution path, not just the CLI flags:

- A `1`-GPU run with `docker://nvidia/cuda:12.8.1-base-ubuntu22.04` started successfully and saw a live `NVIDIA B200`.
- A `2`-node, `1`-GPU-per-node run with the same CUDA image also started successfully and saw one B200 per task on two distinct hosts.
- Slurm exposes pyxis container flags and `enroot` is available, so containerized jobs are a real first-class path on this cluster.

Operational takeaway:

- container launch works today
- the fastest reliable smoke image is a small CUDA base image, not a full framework image

### Public PyTorch image compatibility test

We tested `docker://pytorch/pytorch:2.6.0-cuda12.6-cudnn9-runtime` on a B200.

What happened:

- the image imported successfully
- `torch.cuda.get_arch_list()` reported:

```text
['sm_50', 'sm_60', 'sm_70', 'sm_75', 'sm_80', 'sm_86', 'sm_90']
```

- `torch.cuda.is_available()` returned `True`
- actual CUDA tensor execution failed with:

```text
RuntimeError: CUDA error: no kernel image is available for execution on the device
```

Operational takeaway:

- this public image is not usable for B200 on this cluster
- a container can look healthy at import time and still be the wrong runtime for the GPU architecture

Root cause:

- the wheel was built without `sm_100` kernels
- B200 is a Blackwell GPU with compute capability `sm_100`
- runtime detection alone is not enough; the image must actually ship kernels for the target architecture

### Working public PyTorch image

We then tested `docker://pytorch/pytorch:2.7.0-cuda12.8-cudnn9-runtime`.

What happened:

- the image imported successfully
- `torch.version.cuda` reported `12.8`
- `torch.cuda.get_arch_list()` reported:

```text
['sm_75', 'sm_80', 'sm_86', 'sm_90', 'sm_100', 'sm_120', 'compute_120']
```

- `torch.cuda.is_available()` returned `True`
- a real CUDA tensor allocation and reduction succeeded on B200

Operational takeaway:

- the problem was not "PyTorch containers do not work on this cluster"
- the problem was specifically the older `2.6 + cu126` runtime, which predates Blackwell support
- `2.7 + cu128` is the first working public PyTorch path we have validated locally

### Public PyTorch distributed NCCL limitation

We then pushed the same public `2.7 + cu128` image into distributed testing.

What happened:

- a single-GPU CUDA smoke test worked
- a `2`-task, `1`-GPU-per-task NCCL launch through `srun` exposed a Slurm/pyxis quirk where each task saw `CUDA_VISIBLE_DEVICES=0`
- that rank-per-GPU launch path failed inside NCCL with `invalid device ordinal`
- a node-local launcher shape fixed the device mapping issue, but the public image still hung on a trivial NCCL all-reduce even on a single node with `2` GPUs

Operational takeaway:

- `pytorch/pytorch:2.7.0-cuda12.8-cudnn9-runtime` is good enough for single-GPU B200 smoke tests
- it is not a good distributed NCCL demo image on this cluster
- for pyxis container jobs on this cluster, a node-local launcher shape is safer than `srun` rank-per-GPU when you need multi-GPU NCCL inside one node

### Distributed bootstrap findings

We validated the control-plane side of distributed launch separately from the NCCL data plane.

What happened:

- shared-file rendezvous under `/mnt/sharefs/user44/...` worked across `2` nodes
- a `2`-node, `1`-rank-per-node Gloo all-reduce completed successfully
- hostname-based rendezvous inside containers is brittle on this cluster

Operational takeaway:

- for containerized multi-node runs, Fuse should not rely on hostname resolution inside the runtime
- a safer bootstrap is explicit IP injection or a shared-file rendezvous written by the launcher
- the distributed bootstrap problem is solved well enough for the POC; the remaining work is transport selection for NCCL

### NGC / NVCR findings

We tested the path toward `nvcr.io/nvidia/pytorch:25.02-py3` and completed it successfully.

What happened:

- anonymous authentication to `nvcr.io` succeeded
- pyxis was able to import and save the image to `$HOME/fuse-ngc-pytorch-2502.sqsh`
- the staged image is about `23G`
- the first-time import/save path took minutes, which is why live-pulling a framework image feels slow
- once staged, the local `.sqsh` path is fast to reuse

Operational takeaway:

- NVCR access works from this cluster
- the right workflow is to pre-stage a shared `.sqsh` once and reuse it for every real run
- the first run is doing image import and extraction work, not just launching Python

### Staged NGC runtime behavior

We then used the staged `nvcr.io/nvidia/pytorch:25.02-py3` image for real NCCL tests.

What happened:

- a `1`-GPU CUDA smoke test worked on B200
- a single-node `2`-GPU NCCL all-reduce completed successfully
- a `2`-node NCCL run over the default RDMA/IB path failed with explicit `NET/IB` queue-pair errors
- a `2`-node NCCL run succeeded when we forced socket transport with:

```bash
NCCL_IB_DISABLE=1
NCCL_SOCKET_IFNAME=enp71s0
```

Operational takeaway:

- the staged NGC image is the fastest credible distributed runtime for the POC
- for this cluster, the reliable `2`-node smoke path today is NGC plus socket transport
- the RDMA/IB path is not demo-ready yet and should be treated as a follow-up debugging item, not a blocker for the hackathon

Current recommendation:

- for quick single-GPU smoke, `docker://pytorch/pytorch:2.7.0-cuda12.8-cudnn9-runtime` is fine
- for any distributed demo, use `$HOME/fuse-ngc-pytorch-2502.sqsh`
- for the current `2`-node demo path, set `NCCL_IB_DISABLE=1` and `NCCL_SOCKET_IFNAME=enp71s0`

Suggested pre-stage command:

```bash
srun --immediate=120 -p priority --gres=gpu:1 --mem=20G --time=00:20:00 \
  --container-image=docker://nvcr.io/nvidia/pytorch:25.02-py3 \
  --container-save=$HOME/fuse-ngc-pytorch-2502.sqsh \
  bash -lc 'python -c "import torch; print(torch.__version__)"'
```

## Why This Matters For Fuse

These measurements give us a concrete demo story:

- Slurm sees a flat `gpu:8` resource.
- Fuse can discover and reason about `NV18` intra-node bandwidth, NUMA boundaries, and NIC locality.
- The POC can show that "topology-aware" is not marketing language; it changes which local GPU indices should carry cross-node traffic.
- The `two_node` QOS is enough to show a real `16`-GPU, `2`-node placement story without needing special access beyond the current hackathon limits.
- The full `16`-GPU slice can be obtained immediately, so the live path does not have to be downgraded to a toy `2 x 1` test.
- For this workstream, `user44` is effectively the shared cluster principal across login and compute nodes.
- That makes parallel agent operations realistic, but it also means raw Slurm commands are not enough as a coordination model.
- The real differentiator is not that an agent can shell out to `sbatch`; it is that many logical actors can share the same credential while Fuse carries actor IDs, idempotency keys, leases, and audit history above it.
- The validated `sbatch` / `squeue` / `sacct` / `scancel` loop is the substrate; Fuse should add the safer, lower-collision control surface on top.
- Safe parallelism is what turns agent usage into better scheduler utilization instead of a single-controller bottleneck.
- For current validation, keep the workload simple: `makemore` for the fastest smoke path and `nanochat` for the live distributed demo.
- Axolotl or LlamaFactory can be follow-up examples, but they should not be the first thing we debug on the 16-GPU slice.
- The stock image does not include a usable ML runtime, so Fuse should treat container launch or user-managed environments as a first-class workflow.
- The cluster already exposes Slurm container flags, which means Fuse can stay thin and still support a clean `fuse train --image ...` story.
- The smallest useful container smoke test is available right now with a CUDA base image.
- Framework-image compatibility is architecture-sensitive; Fuse should surface that early instead of letting users discover it through runtime crashes.
- The first pull of a large framework image costs minutes because pyxis/enroot is importing and saving the image, not just launching it.
- For this cluster, the likely production path is `fuse train --image /mnt/sharefs/.../image.sqsh`, not `fuse train --image nvcr.io/...` in the hot path.
- For multi-GPU NCCL inside a node, a node-local launcher shape is safer than `srun` rank-per-GPU on this stack.
- For the current hackathon `2`-node demo, the reliable path is staged NGC plus socket transport, not the raw IB path.

## Fuse Train And Shard Proof

We also validated the higher-level CLI sugar against the live cluster instead of stopping at raw `fuse run`:

- `fuse shard --model llama-70b --gpus 16` returned `TP=8 PP=2 DP=1` on the discovered B200 shape.
- `fuse train --example makemore` is a real working path, not just a README verb.
- `fuse train --example nanochat --gpus 2 --steps 10 --hold 60` submitted as Slurm job `1680`, reached `RUNNING` on `us-west-a2-gpu-024`, then canceled cleanly back to Fuse state `CANCELLED`.
- That launch used `torchrun --standalone --nnodes=1 --nproc-per-node=2`, so the current `fuse train` path is real for single-node multi-GPU examples.
- The current multi-node `nanochat` path is still the manual Slurm launcher described in `scripts/workloads/README.md`.
- `fuse train --example axolotl-probe` is useful as a runtime probe, and today it fails on the staged NGC image with `ModuleNotFoundError: No module named 'axolotl'`.

One bug fell out of this proof and is now fixed in code:

- Slurm states like `CANCELLED by 2035` were being normalized to Fuse `FAILED`.
- Fuse now maps that family of terminal states to `CANCELLED`, which is the correct operator-facing behavior for the demo.

## Reproducing The Probe

From your laptop:

```bash
./scripts/slurm-recon.sh --remote user44@184.34.82.180 --mode full
```

From a login node:

```bash
./scripts/slurm-recon.sh --mode full
```

Use `--mode quick` if you only want the cluster inventory and a 1-GPU sanity check.

To run the Fuse-specific smoke suite:

```bash
./scripts/slurm-smoke.sh --remote user44@184.34.82.180 --mode full
```

Or from a login node:

```bash
./scripts/slurm-smoke.sh --mode full
```

## Next Captures

- 2-node NCCL all-reduce bandwidth
- `nvidia-smi nvlink -s` on a full node
- `NCCL_DEBUG=INFO` output from a real 2-node job
- a deliberately bad cross-node placement to compare against a topology-aware placement
