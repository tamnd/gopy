// Package _scproxy is a minimal stub for the macOS _scproxy C module
// (SystemConfiguration proxy settings). It's imported by urllib/request.py
// only when sys.platform == 'darwin'. The stub returns empty settings so
// urllib works without requiring a full SystemConfiguration framework port.
//
// CPython: Modules/_scproxy.c

package _scproxy

import (
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_scproxy", buildModule)
}

func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_scproxy")
	d := m.Dict()

	// _get_proxy_settings() -> dict: returns empty proxy settings.
	// CPython: Modules/_scproxy.c:_scproxy__get_proxy_settings_impl
	set := func(name string, val objects.Object) error {
		return d.SetItem(objects.NewStr(name), val)
	}
	if err := set("_get_proxy_settings", objects.NewBuiltinFunction("_get_proxy_settings",
		func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.NewDict(), nil
		})); err != nil {
		return nil, err
	}
	// _get_proxies() -> dict: returns empty proxy map.
	// CPython: Modules/_scproxy.c:_scproxy__get_proxies_impl
	if err := set("_get_proxies", objects.NewBuiltinFunction("_get_proxies",
		func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.NewDict(), nil
		})); err != nil {
		return nil, err
	}
	return m, nil
}
