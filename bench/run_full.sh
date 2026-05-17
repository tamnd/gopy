#!/usr/bin/env bash
# run_full.sh — full standalone-corpus run across cpython 3.14, PyPy
# 3.11, and gopy. Uses bench/bench_sources/*.py via run_one.sh so
# everyone sees the same workload. Run nightly + as a release gate.
# Wall time: ~15-30 min depending on host.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

OUT_DIR="${OUT_DIR:-$REPO_ROOT/bench}"

CPYTHON_BIN="${CPYTHON_BIN:-$(command -v python3.14 || true)}"
PYPY_BIN="${PYPY_BIN:-$HOME/pypy3.11/pypy3.11-v7.3.22-macos_arm64/bin/pypy}"
GOPY_BIN="${GOPY_BIN:-$REPO_ROOT/bin/gopy}"

# Full bench list: every .py under bench_sources/. Stays in sync with
# the directory automatically as new benches land for P0.4.
FULL_LIST="$(ls "$REPO_ROOT/bench/bench_sources"/*.py | xargs -n1 basename | sed 's/\.py$//' | tr '\n' ',' | sed 's/,$//')"

mkdir -p "$OUT_DIR"

echo "==> bench list: $FULL_LIST"

echo "==> cpython 3.14 (full)"
bench/run_one.sh "$CPYTHON_BIN" cpython314 "$FULL_LIST" "$OUT_DIR/raw_cpython314_full.json"

echo "==> PyPy 3.11 (full)"
if [ -x "$PYPY_BIN" ]; then
    bench/run_one.sh "$PYPY_BIN" pypy311 "$FULL_LIST" "$OUT_DIR/raw_pypy311_full.json"
else
    echo "PyPy not found at $PYPY_BIN — run bench/install_pypy.sh; writing empty placeholder"
    echo '{"interpreter":"pypy311","version":"missing","benchmarks":{}}' > "$OUT_DIR/raw_pypy311_full.json"
fi

echo "==> gopy (full)"
if [ -x "$GOPY_BIN" ]; then
    WARMUPS=1 RUNS=2 \
        BASELINE_JSON="$OUT_DIR/raw_cpython314_full.json" \
        TARGET_WALL_MS="${TARGET_WALL_MS:-30000}" \
        EST_SLOWDOWN="${EST_SLOWDOWN:-300}" \
        bench/run_one.sh "$GOPY_BIN" gopy "$FULL_LIST" "$OUT_DIR/raw_gopy_full.json"
else
    echo "gopy not built at $GOPY_BIN — run 'make build'; writing empty placeholder"
    echo '{"interpreter":"gopy","version":"missing","benchmarks":{}}' > "$OUT_DIR/raw_gopy_full.json"
fi

echo "==> compare (full)"
go run ./bench/cmd/compare \
    -cpy  "$OUT_DIR/raw_cpython314_full.json" \
    -pypy "$OUT_DIR/raw_pypy311_full.json" \
    -gopy "$OUT_DIR/raw_gopy_full.json" \
    -out  "$OUT_DIR/results_full.md"

cat "$OUT_DIR/results_full.md"
