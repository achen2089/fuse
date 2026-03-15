#!/usr/bin/env bash

set -euo pipefail

mode="quick"
remote=""

usage() {
  cat <<'EOF'
Usage:
  ./scripts/slurm-smoke.sh [--mode quick|full] [--remote user@host]

Examples:
  ./scripts/slurm-smoke.sh --mode quick
  ./scripts/slurm-smoke.sh --mode full --remote user44@184.34.82.180

Notes:
  - Run this from the repo root.
  - Without --remote, the script expects to already be on a Slurm login node.
  - quick mode checks: 2-node sanity, runtime availability, container flags, CUDA container launch, sbatch submit/query.
  - full mode adds: full 16-GPU slice sanity, 2-node CUDA container launch, and sbatch cancel smoke.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --mode)
      mode="${2:-}"
      shift 2
      ;;
    --remote)
      remote="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

if [[ "$mode" != "quick" && "$mode" != "full" ]]; then
  echo "invalid mode: $mode" >&2
  usage >&2
  exit 1
fi

run_cmd() {
  local cmd="$1"
  if [[ -n "$remote" ]]; then
    local quoted
    printf -v quoted '%q' "$cmd"
    ssh "$remote" "bash -lc $quoted"
  else
    bash -lc "$cmd"
  fi
}

section() {
  printf '\n### %s ###\n' "$1"
}

section "identity"
run_cmd "hostname; whoami"

section "two-node-one-gpu-sanity"
run_cmd "srun --immediate=60 -p priority --nodes=2 --ntasks=2 --ntasks-per-node=1 --gres=gpu:1 --mem=20G --cpus-per-task=4 --time=00:03:00 bash -lc 'echo HOST=\$(hostname) PROCID=\$SLURM_PROCID NODEID=\$SLURM_NODEID LOCALID=\$SLURM_LOCALID; nvidia-smi --query-gpu=index,name,memory.total --format=csv,noheader'"

section "runtime-availability"
run_cmd "srun --immediate=60 --gres=gpu:1 --mem=10G --time=00:03:00 bash -lc 'hostname; command -v python3; python3 --version; command -v torchrun || echo no_torchrun; command -v all_reduce_perf || echo no_all_reduce_perf; command -v conda || echo no_conda; python3 -c \"import torch; print(torch.__version__)\" 2>/dev/null || echo no_torch'"

section "container-support"
run_cmd "srun --help 2>&1 | grep -i container | sed -n '1,40p'; echo '---'; command -v enroot || echo no_enroot; command -v docker || echo no_docker; command -v nerdctl || echo no_nerdctl; command -v podman || echo no_podman; command -v apptainer || echo no_apptainer; command -v singularity || echo no_singularity"

section "container-one-gpu-cuda"
run_cmd "srun --immediate=60 -p priority --gres=gpu:1 --mem=10G --time=00:03:00 --container-image=docker://nvidia/cuda:12.8.1-base-ubuntu22.04 bash -lc 'echo HOST=\$(hostname); nvidia-smi -L'"

section "sbatch-submit-query"
smoke_job_id="$(run_cmd "sbatch --parsable -p priority --gres=gpu:1 --mem=10G --time=00:02:00 --output=\$HOME/fuse-smoke-%j.out --wrap='hostname; nvidia-smi --query-gpu=name,memory.total --format=csv,noheader'")"
echo "JOBID=${smoke_job_id}"
sleep 5
run_cmd "squeue -j ${smoke_job_id} -o '%i|%T|%M|%D|%R' -h || true; echo '---'; sacct -j ${smoke_job_id} --format=JobIDRaw,State,ExitCode,NodeList,Start,End -P -n || true; echo '---'; cat \$HOME/fuse-smoke-${smoke_job_id}.out 2>/dev/null || echo output_not_ready"

if [[ "$mode" == "full" ]]; then
  section "two-node-sixteen-gpu-sanity"
  run_cmd "srun --immediate=60 -p priority --nodes=2 --ntasks=2 --ntasks-per-node=1 --gres=gpu:8 --mem=100G --cpus-per-task=16 --time=00:03:00 bash -lc 'echo HOST=\$(hostname) PROCID=\$SLURM_PROCID NODEID=\$SLURM_NODEID; nvidia-smi -L | wc -l'"

  section "two-node-cuda-container"
  run_cmd "srun --immediate=60 -p priority --nodes=2 --ntasks=2 --ntasks-per-node=1 --gres=gpu:1 --mem=20G --cpus-per-task=4 --time=00:05:00 --container-image=docker://nvidia/cuda:12.8.1-base-ubuntu22.04 bash -lc 'echo HOST=\$(hostname) PROCID=\$SLURM_PROCID NODEID=\$SLURM_NODEID; nvidia-smi -L'"

  section "sbatch-cancel"
  cancel_job_id="$(run_cmd "sbatch --parsable -p priority --gres=gpu:1 --mem=10G --time=00:05:00 --output=\$HOME/fuse-cancel-%j.out --wrap='sleep 300'")"
  echo "JOBID=${cancel_job_id}"
  run_cmd "scancel ${cancel_job_id}"
  sleep 2
  run_cmd "squeue -j ${cancel_job_id} -o '%i|%T|%M|%D|%R' -h || true; echo '---'; sacct -j ${cancel_job_id} --format=JobIDRaw,State,ExitCode,NodeList,Start,End -P -n || true; echo '---'; cat \$HOME/fuse-cancel-${cancel_job_id}.out 2>/dev/null || echo output_not_ready"
fi
