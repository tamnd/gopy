# gopy

[![CI](https://github.com/tamnd/gopy/actions/workflows/ci.yml/badge.svg)](https://github.com/tamnd/gopy/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/tamnd/gopy.svg)](https://pkg.go.dev/github.com/tamnd/gopy)
[![Go Report Card](https://goreportcard.com/badge/github.com/tamnd/gopy)](https://goreportcard.com/report/github.com/tamnd/gopy)
[![License: Apache 2.0](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

`gopy` is a from-scratch Go reimplementation of the CPython interpreter
core. The goal is 100% behavioural compatibility with upstream CPython
3.14: same data structures, same models, same wire formats, same error
messages. The only thing that changes is the surface API style, which
adopts Go idioms modelled on the Go standard library.

> **Status: very early.** v0.0.x is project scaffolding. The interpreter
> does not run Python code yet. The roadmap is tracked in the project
> notes and surfaced through the changelog.

## Non-goals

* No alternative semantics. If gopy diverges from CPython, the bug is in
  gopy.
* No CPython C extension ABI. `.so` and `.pyd` modules are out of scope.
  Native modules are reimplemented in Go on demand.
* No Python 2.

## Install

Requires Go 1.26 or newer.

```sh
go install github.com/tamnd/gopy/cmd/gopy@latest
```

Or grab a prebuilt binary from the [releases page](https://github.com/tamnd/gopy/releases).

## Quick start

```sh
$ gopy --version
gopy 0.1.0 (3.14.0+) [go1.26 darwin/arm64]

$ gopy --copyright
Copyright (c) 2026 The gopy Authors. All Rights Reserved.
...
```

## Build from source

```sh
git clone https://github.com/tamnd/gopy
cd gopy
make build
./bin/gopy --version
```

Common developer tasks live in the [Makefile](Makefile):

| Target       | What it does                                      |
| ------------ | ------------------------------------------------- |
| `make build` | Build the `gopy` binary into `./bin/gopy`         |
| `make test`  | Run the unit tests with the race detector         |
| `make cover` | Produce `coverage.txt` and print total coverage   |
| `make vet`   | Run `go vet`                                      |
| `make lint`  | Run `golangci-lint` (must be installed locally)   |
| `make tidy`  | Run `go mod tidy`                                 |

## Repository layout

```
gopy/
  build/              version, platform, compiler, copyright strings
  cmd/gopy/           interpreter entry point (mirrors CPython python.c)
  changelog/          per-release changelog fragments
  .github/            workflows, dependabot, code owners, templates
```

The runtime packages live at the module root (no `internal/`) so that
embedders and companion modules can import them directly.

## Contributing

Patches are welcome. Please read [CONTRIBUTING.md](CONTRIBUTING.md) and
the [Code of Conduct](CODE_OF_CONDUCT.md) before opening a pull request.

Security issues should follow the disclosure procedure in
[SECURITY.md](SECURITY.md).

## License

`gopy` is distributed under the Apache License, Version 2.0. See
[LICENSE](LICENSE) for the full text.

Portions of the design and observable behavior are derived from
[CPython](https://github.com/python/cpython), which is distributed under
the Python Software Foundation License.
