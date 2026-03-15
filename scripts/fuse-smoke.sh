#!/usr/bin/env bash

set -euo pipefail

timeout_seconds=420
poll_seconds=5
job_name="makemore-smoke-$(date +%Y%m%d-%H%M%S)"
image="docker://pytorch/pytorch:2.7.0-cuda12.8-cudnn9-runtime"
shared_root="${FUSE_SHARED_ROOT:-/mnt/sharefs/user44}"
fuse_bin="${FUSE_BIN:-}"
db_path=""
artifacts_dir=""
skip_tests=0
keep_state=0
require_fetch=0

usage() {
  cat <<'EOF'
Usage:
  ./scripts/fuse-smoke.sh [options]

Options:
  --fuse-bin PATH         Path to the Fuse binary. Defaults to ./fuse, then fuse on PATH, else builds a temp binary.
  --db PATH               Local SQLite path for the smoke run.
  --artifacts-dir PATH    Local artifacts/render directory for the smoke run.
  --job-name NAME         Fuse job name. Default: makemore-smoke-YYYYmmdd-HHMMSS
  --image IMAGE           Slurm container image. Default: docker://pytorch/pytorch:2.7.0-cuda12.8-cudnn9-runtime
  --shared-root PATH      Remote shared root. Default: /mnt/sharefs/user44
  --timeout SECONDS       Max time to wait for completion. Default: 420
  --poll SECONDS          Poll interval for fuse why. Default: 5
  --skip-tests            Skip go test preflight.
  --keep-state            Keep local temp db/artifacts directory instead of deleting it.
  --require-fetch         Fail unless the job fetches the makemore repo (git or zip fallback).
  -h, --help              Show this help.

Notes:
  - The canonical repo-local binary is ./fuse.
  - make build also refreshes the legacy ./.bin/fuse-live wrapper for older scripts.
  - Uses the local fuse CLI with its default SSH target.
  - Verifies the full Fuse lifecycle: run -> jobs -> why -> logs.
  - The workload proves CUDA on the real cluster and tries to fetch karpathy/makemore inside the job.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --fuse-bin)
      fuse_bin="${2:-}"
      shift 2
      ;;
    --db)
      db_path="${2:-}"
      shift 2
      ;;
    --artifacts-dir)
      artifacts_dir="${2:-}"
      shift 2
      ;;
    --job-name)
      job_name="${2:-}"
      shift 2
      ;;
    --image)
      image="${2:-}"
      shift 2
      ;;
    --shared-root)
      shared_root="${2:-}"
      shift 2
      ;;
    --timeout)
      timeout_seconds="${2:-}"
      shift 2
      ;;
    --poll)
      poll_seconds="${2:-}"
      shift 2
      ;;
    --skip-tests)
      skip_tests=1
      shift
      ;;
    --keep-state)
      keep_state=1
      shift
      ;;
    --require-fetch)
      require_fetch=1
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

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

state_root=""
build_root=""
cleanup() {
  if [[ -n "$build_root" && -d "$build_root" ]]; then
    rm -rf "$build_root"
  fi
  if [[ "$keep_state" -eq 0 && -n "$state_root" && -d "$state_root" ]]; then
    rm -rf "$state_root"
  fi
}
trap cleanup EXIT

if [[ -z "$db_path" || -z "$artifacts_dir" ]]; then
  state_root="$(mktemp -d /tmp/fuse-smoke.XXXXXX)"
fi
if [[ -z "$db_path" ]]; then
  db_path="$state_root/state.db"
fi
if [[ -z "$artifacts_dir" ]]; then
  artifacts_dir="$state_root/artifacts"
fi
mkdir -p "$artifacts_dir"

if [[ -z "$fuse_bin" ]]; then
  if [[ -x ./fuse ]]; then
    fuse_bin="./fuse"
  elif command -v fuse >/dev/null 2>&1; then
    fuse_bin="$(command -v fuse)"
  else
    build_root="$(mktemp -d /tmp/fuse-smoke-bin.XXXXXX)"
    fuse_bin="$build_root/fuse"
    go build -o "$fuse_bin" ./cmd/fuse
  fi
fi

if [[ ! -x "$fuse_bin" ]]; then
  echo "fuse binary is not executable: $fuse_bin" >&2
  exit 1
fi

go_cache="${state_root:-/tmp}/go-build"
mkdir -p "$go_cache"

run_fuse() {
  local cmd="$1"
  shift
  "$fuse_bin" "$cmd" --db "$db_path" --artifacts-dir "$artifacts_dir" "$@"
}

python_json_field() {
  local code="$1"
  python3 -c "$code"
}

print_section() {
  printf '\n### %s ###\n' "$1"
}

print_section "config"
printf 'fuse_bin=%s\n' "$fuse_bin"
printf 'db_path=%s\n' "$db_path"
printf 'artifacts_dir=%s\n' "$artifacts_dir"
printf 'job_name=%s\n' "$job_name"
printf 'image=%s\n' "$image"
printf 'shared_root=%s\n' "$shared_root"

if [[ "$skip_tests" -eq 0 ]]; then
  print_section "go-test"
  env GOCACHE="$go_cache" go test ./...
fi

print_section "preflight-status"
status_json="$(run_fuse status --json)"
printf '%s\n' "$status_json" | tee "$artifacts_dir/status.json" >/dev/null
printf '%s\n' "$status_json" | python_json_field 'import json,sys; data=json.load(sys.stdin); print("nodes=%s devices=%s allocated=%s idle=%s" % (data["nodes"], data["devices"], data["allocated"], data["idle"]))'

print_section "preflight-storage"
storage_json="$(run_fuse storage --json)"
printf '%s\n' "$storage_json" | tee "$artifacts_dir/storage.json" >/dev/null
printf '%s\n' "$storage_json" | python_json_field 'import json,sys; data=json.load(sys.stdin); print("path=%s filesystems=%s" % (data["path"], len(data["filesystems"])))'

read -r -d '' job_script <<EOF || true
set -euo pipefail
WORKROOT="${shared_root}/.fuse/work/${job_name}-\${SLURM_JOB_ID}"
export WORKROOT
mkdir -p "\$WORKROOT"
echo "HOST=\$(hostname)"
python - <<'PY'
import torch
print(f"TORCH_VERSION={torch.__version__}")
print(f"CUDA_AVAILABLE={torch.cuda.is_available()}")
if not torch.cuda.is_available():
    raise SystemExit("cuda unavailable")
x = torch.randn(1024, 1024, device="cuda")
print(f"CUDA_DEVICE={torch.cuda.get_device_name(0)}")
print(f"CUDA_SUM={(x @ x).sum().item():.4f}")
PY
fetch_mode="inline"
if command -v git >/dev/null 2>&1; then
  if git clone --depth 1 https://github.com/karpathy/makemore.git "\$WORKROOT/makemore" >/dev/null 2>&1; then
    fetch_mode="git"
  fi
fi
if [[ "\$fetch_mode" == "inline" ]]; then
  if python - <<'PY'
import io
import os
import pathlib
import urllib.request
import zipfile

root = pathlib.Path(os.environ["WORKROOT"])
target = root / "makemore"
url = "https://github.com/karpathy/makemore/archive/refs/heads/master.zip"
data = urllib.request.urlopen(url, timeout=60).read()
with zipfile.ZipFile(io.BytesIO(data)) as zf:
    zf.extractall(root)
for child in root.iterdir():
    if child.name.startswith("makemore-"):
        child.rename(target)
        break
if not target.exists():
    raise SystemExit("makemore checkout missing after zip fallback")
PY
  then
    fetch_mode="zip"
  fi
fi
if [[ "\$fetch_mode" != "inline" ]]; then
  test -f "\$WORKROOT/makemore/README.md"
fi
echo "MAKEMORE_FETCH_MODE=\$fetch_mode"
echo "FUSE_MAKEMORE_SMOKE_OK"
EOF

print_section "submit"
submit_json="$(run_fuse run --json \
  --name "$job_name" \
  --gpus 1 \
  --cpus 4 \
  --mem-mb 16384 \
  --time 00:05:00 \
  --image "$image" \
  --mount "${shared_root}:${shared_root}" \
  --container-workdir "$shared_root" \
  --env PYTHONUNBUFFERED=1 \
  -- \
  bash -lc "$job_script")"
printf '%s\n' "$submit_json" | tee "$artifacts_dir/submit.json" >/dev/null
job_id="$(printf '%s\n' "$submit_json" | python_json_field 'import json,sys; print(json.load(sys.stdin)["id"])')"
slurm_job_id="$(printf '%s\n' "$submit_json" | python_json_field 'import json,sys; print(json.load(sys.stdin).get("slurm_job_id",""))')"
printf 'job_id=%s slurm_job_id=%s\n' "$job_id" "$slurm_job_id"

deadline=$((SECONDS + timeout_seconds))
final_state=""
final_raw=""
while (( SECONDS < deadline )); do
  why_json="$(run_fuse why --json "$job_id")"
  printf '%s\n' "$why_json" > "$artifacts_dir/why-latest.json"
  final_state="$(printf '%s\n' "$why_json" | python_json_field 'import json,sys; print(json.load(sys.stdin)["current_state"])')"
  final_raw="$(printf '%s\n' "$why_json" | python_json_field 'import json,sys; print(json.load(sys.stdin).get("raw_state",""))')"
  printf 'state=%s raw=%s\n' "$final_state" "$final_raw"
  case "$final_state" in
    SUCCEEDED|FAILED|CANCELLED)
      break
      ;;
  esac
  sleep "$poll_seconds"
done

if [[ -z "$final_state" ]]; then
  echo "timed out waiting for job state" >&2
  exit 1
fi

print_section "jobs"
jobs_json="$(run_fuse jobs --json)"
printf '%s\n' "$jobs_json" | tee "$artifacts_dir/jobs.json" >/dev/null
printf '%s\n' "$jobs_json" | python_json_field 'import json,sys; jobs=json.load(sys.stdin); print("jobs_seen=%s" % len(jobs))'

print_section "why"
why_json="$(run_fuse why --json "$job_id")"
printf '%s\n' "$why_json" | tee "$artifacts_dir/why.json" >/dev/null
printf '%s\n' "$why_json" | python_json_field 'import json,sys; data=json.load(sys.stdin); print("reason=%s state=%s raw=%s" % (data["reason_code"], data["current_state"], data.get("raw_state", "")))'

print_section "logs"
logs_json="$(run_fuse logs --json --tail 0 "$job_id")"
printf '%s\n' "$logs_json" | tee "$artifacts_dir/logs.json" >/dev/null
log_content="$(printf '%s\n' "$logs_json" | python_json_field 'import json,sys; print(json.load(sys.stdin)["content"], end="")')"
printf '%s\n' "$log_content"

if [[ "$final_state" != "SUCCEEDED" ]]; then
  echo "job did not succeed: state=$final_state raw=$final_raw" >&2
  exit 1
fi
if [[ "$log_content" != *"CUDA_AVAILABLE=True"* ]]; then
  echo "missing CUDA success marker in logs" >&2
  exit 1
fi
if [[ "$log_content" != *"FUSE_MAKEMORE_SMOKE_OK"* ]]; then
  echo "missing Fuse smoke success marker in logs" >&2
  exit 1
fi

fetch_mode="unknown"
if [[ "$log_content" == *"MAKEMORE_FETCH_MODE=git"* ]]; then
  fetch_mode="git"
elif [[ "$log_content" == *"MAKEMORE_FETCH_MODE=zip"* ]]; then
  fetch_mode="zip"
elif [[ "$log_content" == *"MAKEMORE_FETCH_MODE=inline"* ]]; then
  fetch_mode="inline"
fi

if [[ "$require_fetch" -eq 1 && "$fetch_mode" == "inline" ]]; then
  echo "makemore fetch fallback triggered, but --require-fetch was set" >&2
  exit 1
fi

print_section "result"
printf 'PASS job_id=%s slurm_job_id=%s state=%s fetch_mode=%s\n' "$job_id" "$slurm_job_id" "$final_state" "$fetch_mode"
if [[ "$fetch_mode" == "inline" ]]; then
  printf 'WARN makemore fetch did not complete; inline GPU smoke fallback was used\n'
fi
printf 'local_state=%s\n' "${state_root:-custom}"
