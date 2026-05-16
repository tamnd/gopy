# bench/ — pyperformance harness (spec 1712)

Three-way performance gate: cpython 3.14, PyPy 3.11, gopy.

Layout:

| File                  | Role                                                          |
|-----------------------|---------------------------------------------------------------|
| `install_cpython.sh`  | Create `$HOME/.venv-cpython314`, install `pyperformance`      |
| `install_pypy.sh`     | Download PyPy 3.11 to `$HOME/pypy3.11/`, install pyperformance|
| `run_small.sh`        | Day-to-day gate: 8-benchmark subset across all 3 interpreters |
| `run_full.sh`         | Release/nightly gate: full pyperformance corpus               |
| `run_gopy.sh`         | Shim — drives `bin/gopy` against a bench list, emits JSON     |
| `bench_sources/`      | Self-contained .py copies of each small-subset benchmark      |
| `raw_*.json`          | Per-interpreter wall-clock output (gitignored)                |
| `baseline_v0124.json` | Frozen baseline; CI compares each PR against this             |

Quickstart (assuming `bin/gopy` builds):

    bench/install_cpython.sh
    bench/install_pypy.sh
    make build
    bench/run_small.sh

Output: three `bench/raw_*.json` files plus a markdown table printed
to stdout that drops into the spec's "Current benchmark results"
section.
