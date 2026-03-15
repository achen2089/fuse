#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

host="${1:-user44@184.34.82.180}"
remote_root="${2:-/mnt/sharefs/user44/fuse-workloads}"

COPYFILE_DISABLE=1 COPY_EXTENDED_ATTRIBUTES_DISABLE=1 tar \
  --exclude '._*' \
  -C scripts/workloads -cf - . | \
  ssh "$host" "mkdir -p '$remote_root' && tar -xf - -C '$remote_root' && find '$remote_root' -maxdepth 1 -name '._*' -delete"

echo "staged workload scripts to $host:$remote_root"
