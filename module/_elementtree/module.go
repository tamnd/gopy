// Package _elementtree is the gopy port of CPython's _elementtree
// built-in module, the C accelerator that backs Lib/xml/etree/ElementTree.py.
//
// Phase 1 lands the scaffolding: ParseError exception (subclass of
// SyntaxError, matching xml.etree.ElementTree.ParseError's place in the
// hierarchy), the Element type with tag / text / tail / attrib
// accessors and the positional + **extra constructor, the module-level
// SubElement(parent, tag, attrib=None, **extra) helper, and the
// inittab registration. The full ~4552 LOC port spans subsequent phases
// (children mutation, find/findall via ElementPath, TreeBuilder,
// XMLParser).
//
// CPython: Modules/_elementtree.c:1 _elementtree module
package _elementtree

import (
	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_elementtree", buildModule)
}

// parseErrorType is the ParseError exception, published as
// `_elementtree.ParseError` and re-exported by
// `xml.etree.ElementTree.ParseError`. Subclass of SyntaxError so the
// `code`, `position`, `lineno`, `offset`, and `msg` attributes the
// .position tuple ships from come for free.
//
// CPython: Modules/_elementtree.c:4505 PyErr_NewException
//
//	("xml.etree.ElementTree.ParseError", PyExc_SyntaxError, NULL)
var parseErrorType *objects.Type

func init() {
	parseErrorType = objects.NewType("ParseError", []*objects.Type{errors.PyExc_SyntaxError})
}

// buildModule constructs the _elementtree module dict.
//
// CPython: Modules/_elementtree.c:4495 module_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_elementtree")
	d := m.Dict()

	if err := d.SetItem(objects.NewStr("ParseError"), parseErrorType); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("Element"), elementType); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("SubElement"), objects.NewBuiltinFunction("SubElement", subElement)); err != nil {
		return nil, err
	}

	return m, nil
}
