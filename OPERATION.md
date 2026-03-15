# FluidStack GPU Hackathon Operations Guide

This document is the working guide for accessing the cluster, submitting jobs, and using shared resources responsibly.

## Quick Start

For the current Fuse MVP, the real cluster entrypoint is:

```bash
ssh user44@184.34.82.180
```

Use the same `user44` account across all cluster nodes. That means any login node can be reached as `user44`, and any compute node you enter through Slurm will also use `user44`.

Treat `user44` as the shared cluster principal for this workstream, not the full identity model. If multiple agents or scripts operate in parallel, Fuse should attach separate actor IDs and retry-safe coordination above that transport credential.

The current live-demo assumption is that we are guaranteed **16 GPUs**.

For the current MVP, do **not** run `fuse server` on the real cluster path. `fuse server` is only for local `--faker` simulation.

For the validated cluster inventory and the reproducible probe commands behind these assumptions, see `scripts/README.md` and `scripts/slurm-recon.sh`.

## Cluster Summary

- Scheduler: Slurm
- Compute capacity: 32 GPU compute nodes
- Access layer: 6 login nodes
- Shared storage: 61 TB Lustre filesystem
- GPU software stack: NVIDIA Driver `590.48.01`, CUDA `13.1`
- GPU partitions: `priority` and `unpreemptible`
- Live Fuse cap: QOS `two_node` with `cpu=384, gres/gpu=16, mem=4000G`

## Access

### 1. Generate an SSH key if you do not already have one

```bash
ssh-keygen -t ed25519 -C "your_email@example.com"
cat ~/.ssh/id_ed25519.pub
```

Send the contents of `~/.ssh/id_ed25519.pub` to the organizers.

### 2. Receive your cluster credentials

You will be assigned:

- A username in the format `user01` through `user30`
- A login node to use for SSH access

For the current Fuse workstream, the assigned username is `user44`, and that same username is used across all login nodes and compute nodes in your Slurm allocation.

### 3. Connect to a login node

```bash
ssh <your-username>@<login-node-ip>
```

For the current Fuse workstream, use the quick-start SSH command above. If you switch login nodes, keep the same username: `user44`.

Available login nodes:

- `us-west-a2-login-001` - `35.84.33.219`
- `us-west-a2-login-002` - `44.230.162.249`
- `us-west-a2-login-003` - `44.236.167.7`
- `us-west-a2-login-004` - `54.213.112.223`
- `us-west-a2-login-005` - `184.34.82.180`
- `us-west-a2-login-006` - `184.33.101.123`

Any login node works. Spread out if one is crowded.

## How To Work On The Cluster

Use login nodes for:

- SSH access
- Editing code
- Environment setup
- Launching and monitoring Slurm jobs

Do not use login nodes for:

- GPU training
- Long-running compute-heavy scripts
- Any workload that should run under Slurm

GPU work belongs on compute nodes obtained through `srun`, `salloc`, or `sbatch`.

## Slurm Workflows

### Quick interactive session

Use this for short debugging or quick experiments on a single GPU.

```bash
srun --gpus=1 --pty bash
```

To request more resources explicitly:

```bash
srun --gpus=4 --cpus-per-gpu=16 --mem=128G --time=01:00:00 --pty bash
```

### Interactive allocation for multi-step work

Use this when you want to reserve resources first and run multiple commands inside the allocation.

```bash
salloc --gpus=2 --time=02:00:00
srun python train.py
exit
```

If you no longer need the allocation, release it promptly.

### Current Fuse working plan

For the current Fuse build, target a 16-GPU allocation and inspect the topology before starting the live demo or long run.

```bash
salloc -p priority --gpus=16 --time=04:00:00
scontrol show hostnames "$SLURM_JOB_NODELIST"
srun --ntasks-per-node=1 nvidia-smi topo -m
```

Guidance:

- `two_node` is a QOS limit, not a partition name
- Best case for the live Fuse story is a clean `2 x 8-GPU` slice
- If the allocation is fragmented, keep the live run inside the largest contiguous island and use recorded or simulated output for the larger topology story
- Prepare code and environments on the login node before requesting the 16 GPUs so the allocation does not sit idle

### Container images

The base compute image is intentionally thin, so assume a container-first workflow for real ML runs.

What we validated:

- Slurm supports pyxis-style `--container-image` runs.
- A small CUDA base image works today as a smoke test.
- A public PyTorch `2.6.0 + cu126` image is not usable on B200 for real CUDA execution because it does not ship `sm_100` kernels.
- A public PyTorch `2.7.0 + cu128` image does work on B200 for real CUDA tensor execution.
- The public `2.7.0 + cu128` image is not a reliable distributed NCCL demo image on this cluster.
- A staged NGC PyTorch `25.02` image works for single-node `2`-GPU NCCL.
- A staged NGC PyTorch `25.02` image also works for `2`-node NCCL when we force socket transport.

Operational recommendation:

- pre-stage the real runtime as a shared `.sqsh` image
- point Fuse at the local image path during the demo
- do not live-pull a large framework image during the demo; the first import/save pass costs minutes
- use node-local worker launch for multi-GPU jobs inside one node
- for the current `2`-node smoke path, force socket transport instead of debugging IB in the hot path

### Current validation workloads

For the current Fuse validation and demo path, keep the workload simple:

- `makemore` is the fastest smoke test and easiest bring-up path
- `nanochat` is the preferred live training example; the current `fuse train` path is single-node and the `2`-node path is still manual Slurm
- Axolotl or LlamaFactory are optional follow-up examples only if the simple path is already stable
- `fuse train --example ... --hold N` is the fastest proof that a training-shaped job can launch, reach `RUNNING`, and be canceled cleanly

Example pre-stage command:

```bash
srun --immediate=120 -p priority --gres=gpu:1 --mem=20G --time=00:20:00 \
  --container-image=docker://nvcr.io/nvidia/pytorch:25.02-py3 \
  --container-save=$HOME/fuse-ngc-pytorch-2502.sqsh \
  bash -lc 'python -c "import torch; print(torch.__version__)"'
```

Example launch shape:

```bash
srun -p priority --gpus=1 --container-image=$HOME/fuse-ngc-pytorch-2502.sqsh ...
```

Fastest known working public image:

```bash
srun -p priority --gpus=1 --container-image=docker://pytorch/pytorch:2.7.0-cuda12.8-cudnn9-runtime ...
```

Fastest known working distributed demo shape:

```bash
srun --immediate=120 -p priority \
  --nodes=2 --ntasks=2 --ntasks-per-node=1 \
  --gres=gpu:1 --mem=20G --cpus-per-task=4 --time=00:05:00 \
  --container-image=$HOME/fuse-ngc-pytorch-2502.sqsh \
  --container-mount-home \
  bash -lc '
    export NCCL_IB_DISABLE=1
    export NCCL_SOCKET_IFNAME=enp71s0
    export FUSE_RDZV=file:///mnt/sharefs/user44/fuse-demo-rdzv-${SLURM_JOB_ID}
    export FUSE_WORLD_SIZE=2
    export FUSE_NODE_RANK=$SLURM_PROCID
    python /mnt/sharefs/user44/fuse-nccl-localspawn-f32.py
  '
```

Why this is the fast path:

- The staged `.sqsh` avoids repeated image import cost.
- The public `2.7 + cu128` image is good for CUDA smoke, but its NCCL behavior was not reliable enough for the demo.
- The default multi-node RDMA/IB path produced queue-pair errors on this cluster.
- The socket fallback completed cleanly across `2` nodes and is good enough for a real POC.

### Real path vs faker path

The real path is plain Slurm on the cluster. The faker path is local-only simulation.

Real path:

```bash
ssh user44@184.34.82.180
salloc -p priority --gpus=16 --time=04:00:00
scontrol show hostnames "$SLURM_JOB_NODELIST"
srun --ntasks-per-node=1 nvidia-smi topo -m
```

Faker path:

```bash
fuse server --faker
```

Implications:

- Do not start `fuse server` on the cluster for the current MVP
- Use remote-valid paths for `workdir`, logs, and checkpoint directories
- The remote shared root for the current account is `/mnt/sharefs/user44`
- Treat the faker as the scale/simulation layer and Slurm as the real execution layer
- If multiple agents or scripts touch the same jobs, prefer `--json`, `fuse why`, and retry-safe commands over shell parsing
- Treat `user44` as shared transport; the real actor identity should come from Fuse metadata, not the Unix username alone
- The direct Fuse CLI now defaults to `user44@184.34.82.180`; use `--ssh-host` or `FUSE_SSH_HOST` only when you need to override it

Useful commands around the real path:

```bash
fuse run --gpus 1 --time 00:05:00 -- /bin/true
fuse train --example makemore --steps 80
fuse train --example nanochat --gpus 2 --steps 10 --hold 60
fuse submit job.json
fuse jobs
fuse logs <job-id>
fuse why <job-id>
fuse storage
fuse topo --gpus 8
fuse shard --model llama-70b --gpus 16
fuse train --example makemore --steps 80
fuse topo --job <job-id>
```

End-to-end Fuse smoke against the live cluster:

```bash
./scripts/fuse-smoke.sh
```

That script uses the local `fuse` binary, the default `user44@184.34.82.180` SSH path, the validated public PyTorch `2.7.0 + cu128` image, and checks the full Fuse lifecycle for one real GPU job.

### Batch jobs for unattended runs

Create a job script such as `train.sh`:

```bash
#!/bin/bash
#SBATCH --job-name=my-training
#SBATCH --gpus=4
#SBATCH --cpus-per-gpu=16
#SBATCH --mem=256G
#SBATCH --time=04:00:00
#SBATCH --output=logs/%x-%j.out
#SBATCH --error=logs/%x-%j.err

mkdir -p logs
python train.py --config config.yaml
```

Submit it with:

```bash
sbatch train.sh
```

### Useful Slurm commands

```bash
squeue -u $USER
scancel <job-id>
sinfo
```

## Shared Storage

For the current account, the effective shared home/root is `/mnt/sharefs/user44`.

What that means in practice:

- Files created on one login node are visible from other login and compute nodes
- You usually do not need to copy files between nodes
- Fuse should use paths under `/mnt/sharefs/user44` when running in SSH-backed mode
- `fuse storage --ssh-host user44@184.34.82.180` is the quickest way to check free space on the shared filesystem from the local control path
- Large intermediate outputs should be cleaned up when no longer needed

## Fair Use And Etiquette

- Request only the GPUs, CPUs, memory, and wall time you actually need
- Always set `--time` on jobs so unused allocations return to the cluster promptly
- Cancel idle interactive allocations instead of leaving them reserved
- Write logs with `--output` and `--error` so failures are debuggable without rerunning immediately
- Be mindful that 30 participants are sharing 32 GPU nodes

## Minimal Checklist

1. SSH to a login node.
2. Prepare code and environment on the login node.
3. Launch work with `srun`, `salloc`, or `sbatch`.
4. Monitor with `squeue -u $USER`.
5. Cancel anything idle and clean up large files when finished.
