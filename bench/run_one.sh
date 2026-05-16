#!/usr/bin/env bash
# run_one.sh — run a comma-separated bench list under ONE interpreter
# binary. Used by run_small.sh to drive cpython 3.14, PyPy 3.11, and
# bin/gopy against the same standalone scripts under bench/bench_sources/.
#
# Usage: run_one.sh <interpreter_path> <interpreter_label> <bench_csv> <output_json>
#
# Emits the same JSON shape as the original run_gopy.sh shim:
#   { "interpreter": "<label>", "version": "<--version>", "benchmarks": {...} }
set -euo pipefail

if [ $# -ne 4 ]; then
    echo "Usage: $0 <interpreter_path> <interpreter_label> <bench_csv> <output_json>" >&2
    exit 2
fi

INTERP="$1"
LABEL="$2"
BENCH_CSV="$3"
OUT="$4"

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC_DIR="${SRC_DIR:-$REPO_ROOT/bench/bench_sources}"
WARMUPS="${WARMUPS:-2}"
RUNS="${RUNS:-3}"
# Optional auto-scaler. When BASELINE_JSON points at cpython results and
# TARGET_WALL_MS is set, each bench gets GOPY_BENCH_SCALE = ceil(
# baseline_mean_ms * EST_SLOWDOWN / TARGET_WALL_MS ). Bench scripts that
# honor the env var reduce their iteration count by that factor; we then
# scale measured wall time back up so cross-interpreter ratios stay valid.
BASELINE_JSON="${BASELINE_JSON:-}"
TARGET_WALL_MS="${TARGET_WALL_MS:-30000}"
EST_SLOWDOWN="${EST_SLOWDOWN:-300}"

if [ ! -x "$INTERP" ]; then
    echo "Interpreter $INTERP not executable" >&2
    exit 1
fi

VERSION="$("$INTERP" --version 2>&1 | head -n1 || echo unknown)"

python3.14 - "$INTERP" "$LABEL" "$VERSION" "$BENCH_CSV" "$OUT" "$SRC_DIR" "$WARMUPS" "$RUNS" "$BASELINE_JSON" "$TARGET_WALL_MS" "$EST_SLOWDOWN" <<'PY'
import json, math, os, subprocess, sys, time

(interp, label, version, bench_csv, out_path, src_dir, warmups, runs,
 baseline_json, target_wall_ms, est_slowdown) = sys.argv[1:]
warmups, runs = int(warmups), int(runs)
target_wall_ms = float(target_wall_ms)
est_slowdown = float(est_slowdown)

baseline = {}
if baseline_json and os.path.exists(baseline_json):
    with open(baseline_json) as f:
        baseline = json.load(f).get("benchmarks", {})

def bench_scale(name):
    if not baseline:
        return 1
    b = baseline.get(name, {})
    if b.get("status") != "ok":
        return 1
    projected = b["mean_ms"] * est_slowdown
    if projected <= target_wall_ms:
        return 1
    return max(1, int(math.ceil(projected / target_wall_ms)))

results = {}
for name in [b.strip() for b in bench_csv.split(",") if b.strip()]:
    script = os.path.join(src_dir, f"{name}.py")
    if not os.path.exists(script):
        results[name] = {"status": "missing_source", "path": script}
        continue
    scale = bench_scale(name)
    env = dict(os.environ)
    if scale > 1:
        env["GOPY_BENCH_SCALE"] = str(scale)
    samples = []
    err = None
    for i in range(warmups + runs):
        t0 = time.perf_counter()
        try:
            cp = subprocess.run([interp, script], capture_output=True, timeout=180, env=env)
        except subprocess.TimeoutExpired:
            err = {"status": "timeout"}
            break
        dt_ms = (time.perf_counter() - t0) * 1000.0 * scale
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
    entry = {
        "status":  "ok",
        "runs":    samples,
        "min_ms":  samples[0],
        "mean_ms": sum(samples) / len(samples),
    }
    if scale > 1:
        entry["scale"] = scale
    results[name] = entry

with open(out_path, "w") as f:
    json.dump({
        "interpreter": label,
        "version":     version.strip(),
        "benchmarks":  results,
    }, f, indent=2)
PY

echo "$LABEL run complete → $OUT"
