#!/usr/bin/env bash
# run_small.sh — small-subset, three-way bench (cpython 3.14, PyPy 3.11, gopy).
# This is the day-to-day P0 gate. Same standalone scripts under
# bench/bench_sources/ feed all three interpreters via run_one.sh, so
# the wall-clock comparison is apples-to-apples.
# Wall time: ~1-2 min total.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

BENCH_SMALL="${BENCH_SMALL:-nbody,fannkuch,richards,regex_compile,json_dumps,unpack_sequence,call_method,pidigits}"
OUT_DIR="${OUT_DIR:-$REPO_ROOT/bench}"

CPYTHON_BIN="${CPYTHON_BIN:-$(command -v python3.14 || true)}"
PYPY_BIN="${PYPY_BIN:-$HOME/pypy3.11/pypy3.11-v7.3.22-macos_arm64/bin/pypy}"
GOPY_BIN="${GOPY_BIN:-$REPO_ROOT/bin/gopy}"

mkdir -p "$OUT_DIR"

echo "==> cpython 3.14"
bench/run_one.sh "$CPYTHON_BIN" cpython314 "$BENCH_SMALL" "$OUT_DIR/raw_cpython314.json"

echo "==> PyPy 3.11"
if [ -x "$PYPY_BIN" ]; then
    bench/run_one.sh "$PYPY_BIN" pypy311 "$BENCH_SMALL" "$OUT_DIR/raw_pypy311.json"
else
    echo "PyPy not found at $PYPY_BIN — run bench/install_pypy.sh; writing empty placeholder"
    echo '{"interpreter":"pypy311","version":"missing","benchmarks":{}}' > "$OUT_DIR/raw_pypy311.json"
fi

echo "==> gopy"
if [ -x "$GOPY_BIN" ]; then
    # gopy is ~500x slower today; give it 1 warmup + 2 runs to keep the
    # small-subset run under ~10 min. cpython/PyPy stay at 2 warmups + 3 runs.
    # Auto-scale per-bench iteration counts via GOPY_BENCH_SCALE so wall time
    # per bench stays under TARGET_WALL_MS (default 30s). run_one.sh reads
    # the freshly written cpython baseline to project gopy's wall time.
    WARMUPS=1 RUNS=2 \
        BASELINE_JSON="$OUT_DIR/raw_cpython314.json" \
        TARGET_WALL_MS="${TARGET_WALL_MS:-30000}" \
        EST_SLOWDOWN="${EST_SLOWDOWN:-300}" \
        bench/run_one.sh "$GOPY_BIN" gopy "$BENCH_SMALL" "$OUT_DIR/raw_gopy.json"
else
    echo "gopy not built at $GOPY_BIN — run 'make build'; writing empty placeholder"
    echo '{"interpreter":"gopy","version":"missing","benchmarks":{}}' > "$OUT_DIR/raw_gopy.json"
fi

echo "==> compare"
go run ./bench/cmd/compare \
    -cpy  "$OUT_DIR/raw_cpython314.json" \
    -pypy "$OUT_DIR/raw_pypy311.json" \
    -gopy "$OUT_DIR/raw_gopy.json" \
    -out  "$OUT_DIR/results_small.md"

cat "$OUT_DIR/results_small.md"

echo "==> baseline gate"
go run ./bench/cmd/compare-baseline \
    -current "$OUT_DIR/raw_gopy.json" \
    -baseline "$REPO_ROOT/bench/baseline_v0124.json" \
    -tolerance "${BASELINE_TOLERANCE:-0.10}"
