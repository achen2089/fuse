#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

canonical_bin="./fuse"
compat_bin="./.bin/fuse-live"
bin="$canonical_bin"
bin_explicit=0
skip_build=0

usage() {
  cat <<'EOF'
Usage:
  ./scripts/fuse-live-smoke.sh [--bin PATH] [--skip-build]

Options:
  --bin PATH    Fuse binary to run. Default: ./fuse. Legacy compat alias: ./.bin/fuse-live
  --skip-build  Skip the local build step.

What it validates:
  - live cluster discovery through the Fuse CLI
  - storage and topology probes
  - plain host-backed job submission
  - containerized NGC submission
  - container env injection and workdir/mount wiring

Notes:
  - The canonical repo-local binary is ./fuse.
  - make build also refreshes the legacy ./.bin/fuse-live wrapper for older scripts.
  - This script runs the local Fuse binary, which SSHes to the live Slurm cluster.
  - It expects the staged NGC image at /mnt/sharefs/user44/fuse-ngc-pytorch-2502.sqsh.
  - It uses unique job names based on the current timestamp.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --bin)
      bin="${2:-}"
      bin_explicit=1
      shift 2
      ;;
    --skip-build)
      skip_build=1
      shift
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

section() {
  printf '\n### %s ###\n' "$1"
}

if [[ "$skip_build" -eq 0 ]]; then
  section "build"
  if [[ "$bin" == "$canonical_bin" || "$bin" == "$compat_bin" ]]; then
    make build
  else
    mkdir -p "$(dirname "$bin")"
    go build -o "$bin" ./cmd/fuse
  fi
fi

if [[ "$bin_explicit" -eq 0 && ! -x "$bin" ]] && command -v fuse >/dev/null 2>&1; then
  bin="$(command -v fuse)"
fi

if [[ ! -x "$bin" ]]; then
  echo "fuse binary is not executable: $bin" >&2
  exit 1
fi

suffix="$(date +%s)"
host_job="live-smoke-${suffix}"
container_job="ngc-smoke-${suffix}"
env_job="env-smoke-${suffix}"

section "status"
"$bin" status

section "storage"
"$bin" storage

section "topology"
"$bin" topo --gpus 8 --cpus 16 --mem-mb 102400 --time 00:03:00 --immediate 60

section "host-submit"
"$bin" run --name "$host_job" --gpus 1 --time 00:02:00 -- /bin/true
sleep 5
"$bin" why "$host_job"

section "container-submit"
"$bin" run \
  --name "$container_job" \
  --gpus 1 \
  --time 00:03:00 \
  --image /mnt/sharefs/user44/fuse-ngc-pytorch-2502.sqsh \
  --mount-home \
  -- bash -lc 'python -c "import torch; print(torch.__version__)"'
sleep 8
"$bin" logs "$container_job" --tail 50
"$bin" why "$container_job"

section "container-env-submit"
"$bin" run \
  --name "$env_job" \
  --gpus 1 \
  --time 00:03:00 \
  --image /mnt/sharefs/user44/fuse-ngc-pytorch-2502.sqsh \
  --mount-home \
  --mount /mnt/sharefs/user44:/mnt/sharefs/user44 \
  --container-workdir /mnt/sharefs/user44 \
  --env NCCL_IB_DISABLE=1 \
  --env NCCL_SOCKET_IFNAME=enp71s0 \
  -- bash -lc 'printf "%s %s %s\n" "$PWD" "$NCCL_IB_DISABLE" "$NCCL_SOCKET_IFNAME"'
sleep 8
"$bin" logs "$env_job" --tail 50
"$bin" why "$env_job"

section "jobs-json"
"$bin" jobs --json
