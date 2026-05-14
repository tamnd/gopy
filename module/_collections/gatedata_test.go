// Shared loader for gate scripts in module/_collections/. See module/io
// for the same pattern.

package _collections_test

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
