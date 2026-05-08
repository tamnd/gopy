// Package regrtest is the gopy port of CPython's test driver
// (Lib/test/regrtest.py). It walks the vendored test corpus under
// test/cpython/, reads the triage manifest, and runs each ready or
// done entry through the gopy binary.
//
// CPython: Lib/test/regrtest.py
package regrtest

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Status is the per-entry triage state read from
// test/cpython/MANIFEST.txt.
type Status string

const (
	// StatusDone is set once the entry runs green under the gopy
	// harness. The runner pins these in CI.
	StatusDone Status = "done"
	// StatusReady means the underlying gopy feature has shipped and
	// the test file is expected to pass once vendored. The runner
	// attempts these but tolerates failure.
	StatusReady Status = "ready"
	// StatusPending means the test was vendored but has known
	// divergences. The runner skips these and the manifest carries
	// a note.
	StatusPending Status = "pending"
	// StatusDeferred means the underlying gopy feature has not
	// shipped yet (typically a stdlib gap).
	StatusDeferred Status = "deferred"
	// StatusOutOfScope means the test targets CPython internals
	// that gopy will never carry (C-API, ctypes, GUI, gdb, ...).
	StatusOutOfScope Status = "out-of-scope"
)

// Entry is one row of the triage manifest.
type Entry struct {
	// Name is the test module / package name as it appears in
	// CPython's Lib/test/, e.g. "test_grammar" or "test_email".
	Name string
	// Status is the current triage state.
	Status Status
	// Version is the gopy release that owns the underlying feature
	// (e.g. "v0.5.0"). Empty for out-of-scope entries.
	Version string
	// Note is an optional one-line comment carried in the manifest.
	Note string
}

// IsPackage reports whether the entry refers to a Lib/test/test_X/
// directory rather than a single test_X.py file.
func (e Entry) IsPackage() bool {
	return strings.HasSuffix(e.Name, "/")
}

// Manifest is the parsed contents of test/cpython/MANIFEST.txt.
type Manifest struct {
	Entries []Entry
}

// ByName returns the entry for the named test, or false if no such
// entry exists.
func (m *Manifest) ByName(name string) (Entry, bool) {
	for _, e := range m.Entries {
		if e.Name == name {
			return e, true
		}
	}
	return Entry{}, false
}

// CountByStatus tallies the entries by their triage state.
func (m *Manifest) CountByStatus() map[Status]int {
	counts := make(map[Status]int)
	for _, e := range m.Entries {
		counts[e.Status]++
	}
	return counts
}

// Filter returns the entries whose status is in the keep set.
func (m *Manifest) Filter(keep ...Status) []Entry {
	want := make(map[Status]bool, len(keep))
	for _, s := range keep {
		want[s] = true
	}
	out := make([]Entry, 0, len(m.Entries))
	for _, e := range m.Entries {
		if want[e.Status] {
			out = append(out, e)
		}
	}
	return out
}

// ParseManifest reads a manifest from r. Each non-blank, non-comment
// line carries tab-separated fields: name, status, version, [note].
// Lines starting with "#" are comments and are skipped.
//
// The parser is intentionally strict: an unknown status or a
// duplicate name is a hard error rather than a silent drop, so a
// typo cannot quietly fall out of CI.
func ParseManifest(r io.Reader) (*Manifest, error) {
	m := &Manifest{}
	seen := make(map[string]int) // name -> first line number
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimRight(sc.Text(), "\r")
		trimmed := strings.TrimSpace(text)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Split(text, "\t")
		if len(fields) < 3 || len(fields) > 4 {
			return nil, fmt.Errorf("manifest: line %d: want 3 or 4 tab-separated fields, got %d", line, len(fields))
		}
		name := strings.TrimSpace(fields[0])
		status := Status(strings.TrimSpace(fields[1]))
		version := strings.TrimSpace(fields[2])
		note := ""
		if len(fields) == 4 {
			note = strings.TrimSpace(fields[3])
		}
		if !knownStatus(status) {
			return nil, fmt.Errorf("manifest: line %d: unknown status %q", line, status)
		}
		if prev, ok := seen[name]; ok {
			return nil, fmt.Errorf("manifest: line %d: duplicate entry %q (first seen on line %d)", line, name, prev)
		}
		seen[name] = line
		m.Entries = append(m.Entries, Entry{
			Name:    name,
			Status:  status,
			Version: version,
			Note:    note,
		})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(m.Entries) == 0 {
		return nil, errors.New("manifest: no entries")
	}
	sort.SliceStable(m.Entries, func(i, j int) bool {
		return m.Entries[i].Name < m.Entries[j].Name
	})
	return m, nil
}

func knownStatus(s Status) bool {
	switch s {
	case StatusDone, StatusReady, StatusPending, StatusDeferred, StatusOutOfScope:
		return true
	}
	return false
}
