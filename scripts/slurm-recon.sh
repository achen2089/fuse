#!/usr/bin/env bash

set -euo pipefail

mode="quick"
remote=""
slurm_user=""

usage() {
  cat <<'EOF'
Usage:
  ./scripts/slurm-recon.sh [--mode quick|full] [--remote user@host] [--user slurm_user]

Examples:
  ./scripts/slurm-recon.sh --mode quick
  ./scripts/slurm-recon.sh --mode full --remote user44@184.34.82.180

Notes:
  - Run this from the repo root.
  - Without --remote, the script expects to already be on a Slurm login node.
  - --mode quick captures cluster inventory, QOS, and a 1-GPU sanity probe.
  - --mode full adds the 8-GPU topology probe and IB status probe.
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
    --user)
      slurm_user="${2:-}"
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

if [[ -z "$slurm_user" ]]; then
  slurm_user="${USER}"
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

section "cluster-summary"
run_cmd "sinfo -s"
run_cmd "sinfo -o \"%N %G %f %m %e\""

section "user-and-qos"
run_cmd "sacctmgr show user ${slurm_user} withassoc"
run_cmd "sacctmgr -nP show qos two_node format=Name%20,MaxTRESPU%100,MaxWall,Flags%30"

section "one-gpu-probe"
run_cmd "srun --immediate=60 --gres=gpu:1 --mem=10G --time=00:03:00 bash -lc 'hostname; nvidia-smi --query-gpu=index,name,memory.total,driver_version --format=csv,noheader; echo \"---\"; nvidia-smi topo -m'"

if [[ "$mode" == "full" ]]; then
  section "eight-gpu-topology"
  run_cmd "srun --immediate=60 --gres=gpu:8 --mem=100G --time=00:05:00 --cpus-per-task=16 bash -lc 'hostname; nvidia-smi --query-gpu=index,name,memory.total --format=csv,noheader; echo \"---\"; nvidia-smi topo -m'"

  section "ib-status"
  run_cmd "srun --immediate=60 --gres=gpu:1 --mem=10G --time=00:02:00 bash -lc 'ibstat || true; echo \"---\"; ibstatus || true'"
fi
