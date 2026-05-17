#!/usr/bin/env bash
# run_gopy.sh — run a comma-separated bench list under bin/gopy.
#
# Usage: run_gopy.sh <bench_csv> <output_json>
#
# Looks up each bench under bench/bench_sources/<name>.py, runs it with
# bin/gopy 3 times after 2 warmup runs, and writes a JSON object shaped
# like pyperformance's `--output` to <output_json>:
#
#   {
#     "interpreter": "gopy",
#     "version":     "<git describe>",
#     "benchmarks": {
#       "nbody":   { "mean_ms": 12.34, "min_ms": 12.01, "runs": [...], "status": "ok" },
#       "richards":{ "status": "import_error", "missing_module": "asyncio" },
#       ...
#     }
#   }
#
# If a bench cannot run (missing module, parse error, panic), the entry
# carries "status" != "ok" and the table renderer prints "N/A".
set -euo pipefail

if [ $# -ne 2 ]; then
    echo "Usage: $0 <bench_csv> <output_json>" >&2
    exit 2
fi

BENCH_CSV="$1"
OUT="$2"

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GOPY_BIN="${GOPY_BIN:-$REPO_ROOT/bin/gopy}"
SRC_DIR="${SRC_DIR:-$REPO_ROOT/bench/bench_sources}"
WARMUPS="${WARMUPS:-2}"
RUNS="${RUNS:-3}"

if [ ! -x "$GOPY_BIN" ]; then
    echo "gopy binary missing at $GOPY_BIN — run 'make build' first" >&2
    exit 1
fi

GIT_VER="$(git -C "$REPO_ROOT" describe --always --dirty 2>/dev/null || echo unknown)"

python3.14 - "$BENCH_CSV" "$OUT" "$GOPY_BIN" "$SRC_DIR" "$WARMUPS" "$RUNS" "$GIT_VER" <<'PY'
import json, os, subprocess, sys, time

bench_csv, out_path, gopy, src_dir, warmups, runs, git_ver = sys.argv[1:]
warmups, runs = int(warmups), int(runs)

results = {}
for name in [b.strip() for b in bench_csv.split(",") if b.strip()]:
    script = os.path.join(src_dir, f"{name}.py")
    if not os.path.exists(script):
        results[name] = {"status": "missing_source", "path": script}
        continue
    samples = []
    err = None
    for i in range(warmups + runs):
        t0 = time.perf_counter()
        try:
            cp = subprocess.run([gopy, script], capture_output=True, timeout=180)
        except subprocess.TimeoutExpired:
            err = {"status": "timeout"}
            break
        dt_ms = (time.perf_counter() - t0) * 1000.0
        if cp.returncode != 0:
            tail = (cp.stderr or b"").decode("utf-8", "replace")[-400:]
            err = {"status": "runtime_error", "exit": cp.returncode, "stderr_tail": tail}
            break
        if i >= warmups:
            samples.append(dt_ms)
    if err is not None:
        results[name] = err
        continue
    samples.sort()
    results[name] = {
        "status":  "ok",
        "runs":    samples,
        "min_ms":  samples[0],
        "mean_ms": sum(samples) / len(samples),
    }

with open(out_path, "w") as f:
    json.dump({
        "interpreter": "gopy",
        "version":     git_ver,
        "benchmarks":  results,
    }, f, indent=2)
PY

echo "gopy run complete → $OUT"
