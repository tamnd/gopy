# Contributing to gopy

Thanks for your interest in gopy. This document describes how to set up a
development environment, the conventions the project follows, and how to
get a change merged.

## Ground rules

1. **CPython is the oracle.** `gopy` aims for byte-identical behaviour
   with CPython 3.14. When in doubt, the behaviour of the C source under
   `cpython/Python/` is authoritative, not the documentation.
2. **Naming, not semantics.** Ports translate identifiers to Go idioms
   but preserve struct layout, field order, and control flow. See the
   naming spec for the translation rules.
3. **Tests first.** New code lands with unit tests in the same package,
   plus a CPython-compatible integration test where applicable.
4. **No em-dashes.** The project style is plain prose. Use periods,
   commas, parentheses, or colons.

## Development environment

You need Go 1.24 or newer. Most checks run from the Makefile:

```sh
make build     # build ./bin/gopy
make test      # unit tests with -race
make vet       # go vet ./...
make lint      # golangci-lint (install separately)
make cover     # coverage.txt
```

A typical loop looks like:

```sh
git checkout -b feat/short-description
# ... edit ...
make fmt vet test
git commit -s -m "build: short imperative summary"
git push -u origin HEAD
gh pr create --fill
```

The `-s` (sign-off) flag appends a `Signed-off-by` trailer asserting the
[Developer Certificate of Origin](https://developercertificate.org/).

## Commit messages

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
type(scope): short summary in the imperative, no trailing period

Optional body wrapped at 72 columns. Explain *why*, not *what*.
The diff already shows what changed.

Refs #123
```

`type` is one of `feat`, `fix`, `perf`, `refactor`, `test`, `docs`,
`build`, `ci`, `chore`. The scope is the package name when it is
obvious (`vm`, `compile`, `gc`, etc.).

## Changelog

Each user-visible change adds a one-line bullet to the `Unreleased`
section in `CHANGELOG.md`. At release time the section is cut into a
file under `changelog/` and dated.

## Pull requests

* Keep PRs focused. One subsystem per PR is the goal.
* CI must be green. The CI workflow runs build, vet, lint, and tests on
  Linux, macOS, and Windows with the two most recent Go releases.
* Reviewers map to packages via `.github/CODEOWNERS`.
* The merge strategy is squash; the squash subject becomes the commit
  in `main`, so write the PR title as you would a commit subject.

## Reporting bugs

Open a GitHub issue with:

* The exact command you ran.
* The output you got and the output you expected.
* The output of `gopy --version`.
* A minimal reproduction, ideally a single Python source snippet.

For security-relevant bugs, follow [SECURITY.md](SECURITY.md) instead of
opening a public issue.
