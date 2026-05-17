#!/usr/bin/env bash
# install_cpython.sh — bootstrap the cpython 3.14 pyperformance venv.
# Idempotent: skips work if the venv + pyperformance are already in place.
set -euo pipefail

VENV="${VENV:-$HOME/.venv-cpython314}"
PYPERF_VERSION="${PYPERF_VERSION:-1.11.0}"

if ! command -v python3.14 >/dev/null 2>&1; then
    echo "python3.14 not on PATH. Install with: brew install python@3.14" >&2
    exit 1
fi

if [ ! -x "$VENV/bin/pyperformance" ]; then
    python3.14 -m venv "$VENV"
    "$VENV/bin/pip" install --upgrade pip
    "$VENV/bin/pip" install "pyperformance==$PYPERF_VERSION"
fi

echo "cpython 3.14 venv ready at $VENV"
"$VENV/bin/python" --version
"$VENV/bin/pyperformance" list >/dev/null && echo "pyperformance: OK"
