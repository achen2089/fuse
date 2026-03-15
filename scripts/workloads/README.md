# Workload Smokes

These are self-contained workload probes for the live Slurm cluster.

They are deliberately small:

- `makemore_smoke.py` is the fastest single-GPU training bring-up path.
- `nanochat_smoke.py` is the minimal language-model training script that can run single-process or distributed.
- `axolotl_probe.py` is an import/runtime probe for an Axolotl image.

## Stage To The Cluster

Push the workload scripts to the shared home on the cluster:

```bash
./scripts/stage-remote-workloads.sh
```

This stages the files under:

```text
/mnt/sharefs/user44/fuse-workloads
```

## Run Through Fuse

The canonical repo-local binary is `./fuse`. If you installed Fuse with `make install`, you can drop the `./`. The generated `./.bin/fuse-live` path exists only as a compatibility alias for older scripts.

`makemore` smoke:

```bash
./fuse train \
  --example makemore \
  --name makemore-smoke \
  --steps 200
```

`nanochat` single-process smoke:

```bash
./fuse train \
  --example nanochat \
  --name nanochat-smoke \
  --steps 40
```

`nanochat` launch-and-cancel proof on one node with `2` GPUs:

```bash
./fuse train \
  --example nanochat \
  --name nanochat-cancel-proof \
  --gpus 2 \
  --steps 10 \
  --hold 60

./fuse why nanochat-cancel-proof
./fuse cancel nanochat-cancel-proof
./fuse why nanochat-cancel-proof
```

That path uses `torchrun --standalone --nnodes=1 --nproc-per-node=2` under the hood.

## Manual 2-Node `nanochat` Probe

The current Fuse CLI does not yet synthesize a multi-node launcher. For the real
distributed bring-up, use Slurm directly with the staged workload:

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

## Axolotl

The current validated cluster path is the staged NGC PyTorch image. Axolotl is
treated as a follow-up probe:

```bash
./fuse train --example axolotl-probe
```

Current result on the staged NGC image:

```text
ModuleNotFoundError: No module named 'axolotl'
```

Run that probe only inside an image that actually contains Axolotl.
