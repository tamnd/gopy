// Shared loader for gate scripts in module/io/. Each .py file under
// gatedata/ is the source of truth for a single parity gate; embedding
// keeps the test binary self-contained while letting the scripts live on
// disk where editors and CPython itself can lint them.

package io_test

import (
	"embed"
	"testing"
)

//go:embed gatedata/*.py
var gateScripts embed.FS

func loadScript(t *testing.T, name string) string {
	t.Helper()
	data, err := gateScripts.ReadFile("gatedata/" + name)
	if err != nil {
		t.Fatalf("load gate script %s: %v", name, err)
	}
	return string(data)
}
