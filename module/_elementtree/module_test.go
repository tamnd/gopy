package _elementtree

import (
	"testing"

	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/objects"
)

// TestBuildModuleAttrs pins the Phase 1 module surface: ParseError,
// Element, SubElement.
func TestBuildModuleAttrs(t *testing.T) {
	m, err := buildModule()
	if err != nil {
		t.Fatalf("buildModule: %v", err)
	}
	d := m.Dict()
	for _, name := range []string{"ParseError", "Element", "SubElement"} {
		if _, err := d.GetItem(objects.NewStr(name)); err != nil {
			t.Errorf("%s missing from module dict: %v", name, err)
		}
	}
}

// TestParseErrorIsSyntaxErrorSubclass pins ParseError's parent. CPython
// publishes ParseError via PyErr_NewException with PyExc_SyntaxError as
// the base, and xml.etree.ElementTree re-uses the same hierarchy so
// downstream `except SyntaxError` clauses catch parse failures.
//
// CPython: Modules/_elementtree.c:4505 PyErr_NewException
func TestParseErrorIsSyntaxErrorSubclass(t *testing.T) {
	if !objects.IsSubtype(parseErrorType, errors.PyExc_SyntaxError) {
		t.Fatalf("ParseError is not a subclass of SyntaxError")
	}
}

// TestElementConstruct exercises the positional tag-only constructor
// and the attrib defaults (text == None, tail == None, attrib == {}).
func TestElementConstruct(t *testing.T) {
	o, err := elementTpNew(elementType, []objects.Object{objects.NewStr("root")}, map[string]objects.Object{})
	if err != nil {
		t.Fatalf("Element('root'): %v", err)
	}
	e, ok := o.(*Element)
	if !ok {
		t.Fatalf("expected *Element, got %T", o)
	}
	if s, _ := e.tag.(*objects.Unicode); s == nil || s.Value() != "root" {
		t.Fatalf("tag: got %v, want 'root'", e.tag)
	}
	if e.text != objects.None() {
		t.Fatalf("text: got %v, want None", e.text)
	}
	if e.tail != objects.None() {
		t.Fatalf("tail: got %v, want None", e.tail)
	}
	if e.attrib == nil || e.attrib.Len() != 0 {
		t.Fatalf("attrib: want empty dict, got %v", e.attrib)
	}
}

// TestElementConstructAttribKeywords pins the **extra folding: keyword
// arguments land on attrib alongside (or in lieu of) the positional
// attrib dict, and `attrib=` kwarg is lifted before the rest fold in.
//
// CPython: Modules/_elementtree.c:374 get_attrib_from_keywords
func TestElementConstructAttribKeywords(t *testing.T) {
	posAttrib := objects.NewDict()
	if err := posAttrib.SetItem(objects.NewStr("class"), objects.NewStr("note")); err != nil {
		t.Fatal(err)
	}
	o, err := elementTpNew(elementType,
		[]objects.Object{objects.NewStr("div"), posAttrib},
		map[string]objects.Object{
			"id":    objects.NewStr("d1"),
			"class": objects.NewStr("hero"),
		})
	if err != nil {
		t.Fatalf("Element(...): %v", err)
	}
	e := o.(*Element)
	cls, err := e.attrib.GetItem(objects.NewStr("class"))
	if err != nil {
		t.Fatalf("attrib['class']: %v", err)
	}
	if s, _ := cls.(*objects.Unicode); s == nil || s.Value() != "hero" {
		t.Fatalf("attrib['class']: got %v, want 'hero' (kwarg should win over positional)", cls)
	}
	id, err := e.attrib.GetItem(objects.NewStr("id"))
	if err != nil {
		t.Fatalf("attrib['id']: %v", err)
	}
	if s, _ := id.(*objects.Unicode); s == nil || s.Value() != "d1" {
		t.Fatalf("attrib['id']: got %v, want 'd1'", id)
	}
}

// TestElementGetSet pins tag / text / tail / attrib read+write through
// the type's Getattro / Setattro slots.
func TestElementGetSet(t *testing.T) {
	o, err := elementTpNew(elementType, []objects.Object{objects.NewStr("root")}, map[string]objects.Object{})
	if err != nil {
		t.Fatal(err)
	}
	e := o.(*Element)

	if err := elementSetattro(e, objects.NewStr("text"), objects.NewStr("hello")); err != nil {
		t.Fatalf("set text: %v", err)
	}
	got, err := elementGetattro(e, objects.NewStr("text"))
	if err != nil {
		t.Fatalf("get text: %v", err)
	}
	if s, _ := got.(*objects.Unicode); s == nil || s.Value() != "hello" {
		t.Fatalf("text: got %v, want 'hello'", got)
	}

	// Deleting tag is rejected with AttributeError, matching CPython.
	if err := elementSetattro(e, objects.NewStr("tag"), nil); err == nil {
		t.Fatalf("expected AttributeError on tag delete")
	}

	// Setting attrib to a non-dict raises TypeError.
	if err := elementSetattro(e, objects.NewStr("attrib"), objects.NewStr("not a dict")); err == nil {
		t.Fatalf("expected TypeError on non-dict attrib")
	}
}

// TestSubElementAppendsChild pins the `SubElement(parent, tag)`
// behavior: a fresh child Element gets appended to parent.children
// and returned.
//
// CPython: Modules/_elementtree.c:606 subelement
func TestSubElementAppendsChild(t *testing.T) {
	parentObj, err := elementTpNew(elementType, []objects.Object{objects.NewStr("root")}, map[string]objects.Object{})
	if err != nil {
		t.Fatal(err)
	}
	parent := parentObj.(*Element)
	childObj, err := subElement(
		[]objects.Object{parent, objects.NewStr("item")},
		map[string]objects.Object{},
	)
	if err != nil {
		t.Fatalf("SubElement: %v", err)
	}
	child, ok := childObj.(*Element)
	if !ok {
		t.Fatalf("SubElement returned %T, want *Element", childObj)
	}
	if len(parent.children) != 1 || parent.children[0] != child {
		t.Fatalf("parent.children: got %v, want [%p]", parent.children, child)
	}
	if s, _ := child.tag.(*objects.Unicode); s == nil || s.Value() != "item" {
		t.Fatalf("child.tag: got %v, want 'item'", child.tag)
	}
}
