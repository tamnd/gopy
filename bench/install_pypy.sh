#!/usr/bin/env bash
# install_pypy.sh — fetch PyPy 3.11 v7.3.22 to $HOME/pypy3.11/.
# Idempotent: skips download/extract if the binary is already present.
set -euo pipefail

PYPY_HOME="${PYPY_HOME:-$HOME/pypy3.11}"
PYPY_VERSION="${PYPY_VERSION:-7.3.22}"
PYPY_ARCH="${PYPY_ARCH:-macos_arm64}"
TARBALL="pypy3.11-v${PYPY_VERSION}-${PYPY_ARCH}.tar.bz2"
URL="https://downloads.python.org/pypy/${TARBALL}"
DIR_NAME="pypy3.11-v${PYPY_VERSION}-${PYPY_ARCH}"
PYPY_BIN="$PYPY_HOME/$DIR_NAME/bin/pypy"
PYPERF_VERSION="${PYPERF_VERSION:-1.11.0}"

mkdir -p "$PYPY_HOME"
cd "$PYPY_HOME"

if [ ! -x "$PYPY_BIN" ]; then
    if [ ! -f "$TARBALL" ]; then
        echo "Downloading $URL"
        curl -L -O "$URL"
    fi
    echo "Extracting $TARBALL"
    tar xjf "$TARBALL"
fi

if ! "$PYPY_BIN" -m pip --version >/dev/null 2>&1; then
    "$PYPY_BIN" -m ensurepip
fi

if ! "$PYPY_BIN" -m pyperformance list >/dev/null 2>&1; then
    "$PYPY_BIN" -m pip install --upgrade pip
    "$PYPY_BIN" -m pip install "pyperformance==$PYPERF_VERSION"
fi

echo "PyPy 3.11 ready at $PYPY_BIN"
"$PYPY_BIN" --version
"$PYPY_BIN" -m pyperformance list >/dev/null && echo "pyperformance: OK"
