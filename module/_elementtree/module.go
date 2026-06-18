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
	// PyErr_NewException builds the class via type(name, (base,), dict),
	// so the new type runs through inherit_slots and picks up the base's
	// tp_new / tp_init / tp_str. NewExcType wires the standard exception
	// slots; copying SyntaxError's TpNew on top mirrors inherit_slots
	// adopting the base's tp_new (a bare objects.NewType leaves TpNew nil,
	// and type_call then refuses construction with "cannot create
	// 'ParseError' instances directly").
	//
	// CPython: Modules/_elementtree.c:4505 PyErr_NewException
	// CPython: Objects/typeobject.c:7521 inherit_slots (tp_new slot)
	parseErrorType = errors.NewExcType("ParseError", []*objects.Type{errors.PyExc_SyntaxError})
	parseErrorType.TpNew = errors.PyExc_SyntaxError.TpNew
	parseErrorType.Str = errors.PyExc_SyntaxError.Str
	// PyErr_NewException splits the dotted name "xml.etree.ElementTree.ParseError"
	// at the last dot: the prefix becomes __module__ and the leaf becomes the
	// class name. traceback.format_exception_only qualifies the printed type as
	// __module__ + '.' + __qualname__, so the module must be set here for the
	// exception to render as "xml.etree.ElementTree.ParseError".
	//
	// CPython: Python/errors.c:911 PyErr_NewExceptionWithDoc (dotted-name split)
	parseErrorType.Module = "xml.etree.ElementTree"
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
