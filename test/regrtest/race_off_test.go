//go:build !race

package regrtest

// raceEnabled reports whether the test binary was built with -race.
// The panel tests exec the gopy interpreter under each test; goid()
// in vm/eval_call.go calls runtime.Stack on every Eval entry, which
// under -race is slow enough that the corpus times out. The CI
// workflow runs the regrtest package in a dedicated no-race step;
// task #639 tracks removing this workaround.
const raceEnabled = false
