// Element is the gopy port of the C ElementObject. It carries a tag,
// optional text + tail strings, an attrib dict, and a (lazy) children
// slice. The CPython struct splits the children + attrib into a heap-
// allocated ElementObjectExtra to keep zero-child elements small;
// gopy carries the fields inline since Go nil slices already cost
// nothing.
//
// JOIN_GET / JOIN_SET tagged-pointer bookkeeping in CPython is used to
// distinguish a list of fragments coming from the TreeBuilder (which
// must be joined into a single string when read) from a real string
// the application assigned. The Phase 1 port keeps text/tail as plain
// objects and defers the join-on-read path until Phase 2 lands the
// TreeBuilder.
//
// CPython: Modules/_elementtree.c:217 ElementObject
// CPython: Modules/_elementtree.c:220 ElementObjectExtra
package _elementtree

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// Element backs `_elementtree.Element`.
type Element struct {
	objects.Header

	tag    objects.Object
	text   objects.Object
	tail   objects.Object
	attrib *objects.Dict
	// children carries direct subelements in document order. Allocated
	// lazily on first append. The CPython STATIC_CHILDREN=4 inline
	// optimization doesn't apply here: Go's append amortizes growth
	// already, and a nil slice header costs nothing on a leaf.
	children []*Element
}

// elementType is the *objects.Type for Element.
var elementType *objects.Type

func init() {
	elementType = objects.NewType("Element", []*objects.Type{objects.ObjectType()})
	elementType.Getattro = elementGetattro
	elementType.Setattro = elementSetattro
	elementType.TpNew = elementTpNew
	elementType.Repr = elementRepr
}

// elementTpNew runs the Python `Element(tag, attrib={}, **extra)`
// constructor. Both `attrib` (positional or kw) and `**extra` merge
// into the final attribute dict; extra wins on key collisions because
// CPython's get_attrib_from_keywords calls PyDict_Update(attrib, kwds).
//
// CPython: Modules/_elementtree.c:413 element_init
// CPython: Modules/_elementtree.c:374 get_attrib_from_keywords
func elementTpNew(_ *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: Element() takes at least 1 positional argument (got %d)", len(args))
	}
	if len(args) > 2 {
		return nil, fmt.Errorf("TypeError: Element() takes at most 2 positional arguments (got %d)", len(args))
	}
	tag := args[0]

	attrib, err := buildAttribDict(args, kwargs)
	if err != nil {
		return nil, err
	}

	e := &Element{
		tag:    tag,
		text:   objects.None(),
		tail:   objects.None(),
		attrib: attrib,
	}
	e.Init(elementType)
	return e, nil
}

// buildAttribDict folds the positional `attrib` argument (when present)
// together with the keyword arguments into a fresh dict. Mirrors the
// CPython sequence: copy(positional) if any, then update with kwds; or
// if only kwds, lift the `attrib` keyword first then update with the
// rest.
//
// CPython: Modules/_elementtree.c:374 get_attrib_from_keywords
// CPython: Modules/_elementtree.c:421 element_init (positional branch)
func buildAttribDict(args []objects.Object, kwargs map[string]objects.Object) (*objects.Dict, error) {
	out := objects.NewDict()

	if len(args) == 2 {
		posAttrib, ok := args[1].(*objects.Dict)
		if !ok {
			return nil, fmt.Errorf("TypeError: attrib must be dict, not '%s'", typeName(args[1]))
		}
		if err := copyDictInto(out, posAttrib); err != nil {
			return nil, err
		}
	} else if kwAttrib, present := kwargs["attrib"]; present {
		d, ok := kwAttrib.(*objects.Dict)
		if !ok {
			return nil, fmt.Errorf("TypeError: attrib must be dict, not '%s'", typeName(kwAttrib))
		}
		if err := copyDictInto(out, d); err != nil {
			return nil, err
		}
		delete(kwargs, "attrib")
	}

	for k, v := range kwargs {
		if k == "attrib" {
			continue
		}
		if err := out.SetItem(objects.NewStr(k), v); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// copyDictInto folds every (key, value) of src into dst, mirroring
// PyDict_Update.
func copyDictInto(dst, src *objects.Dict) error {
	for _, k := range src.Keys() {
		v, err := src.GetItem(k)
		if err != nil {
			return err
		}
		if err := dst.SetItem(k, v); err != nil {
			return err
		}
	}
	return nil
}

// elementGetattro implements `getattr(element, name)` for the four
// getset attributes (tag, text, tail, attrib) plus the standard
// attribute-lookup fallback. The CPython port routes these through
// PyGetSetDef entries on element_getsetlist.
//
// CPython: Modules/_elementtree.c:4305 element_getsetlist
func elementGetattro(o objects.Object, name objects.Object) (objects.Object, error) {
	e, ok := o.(*Element)
	if !ok {
		return objects.GenericGetAttr(o, name)
	}
	n, ok := name.(*objects.Unicode)
	if !ok {
		return objects.GenericGetAttr(o, name)
	}
	switch n.Value() {
	case "tag":
		return e.tag, nil
	case "text":
		return e.text, nil
	case "tail":
		return e.tail, nil
	case "attrib":
		return e.attrib, nil
	}
	return objects.GenericGetAttr(o, name)
}

// elementSetattro implements `setattr(element, name, value)` for the
// four getset attributes. Deletes (value == nil) raise AttributeError
// because CPython's _VALIDATE_ATTR_VALUE rejects them: tag / text /
// tail / attrib cannot be unset, only overwritten.
//
// CPython: Modules/_elementtree.c:2082 _VALIDATE_ATTR_VALUE
// CPython: Modules/_elementtree.c:2091 element_tag_setter
// CPython: Modules/_elementtree.c:2118 element_attrib_setter
func elementSetattro(o objects.Object, name objects.Object, value objects.Object) error {
	e, ok := o.(*Element)
	if !ok {
		return objects.GenericSetAttr(o, name, value)
	}
	n, ok := name.(*objects.Unicode)
	if !ok {
		return objects.GenericSetAttr(o, name, value)
	}
	switch n.Value() {
	case "tag":
		if value == nil {
			return fmt.Errorf("AttributeError: can't delete element attribute")
		}
		e.tag = value
		return nil
	case "text":
		if value == nil {
			return fmt.Errorf("AttributeError: can't delete element attribute")
		}
		e.text = value
		return nil
	case "tail":
		if value == nil {
			return fmt.Errorf("AttributeError: can't delete element attribute")
		}
		e.tail = value
		return nil
	case "attrib":
		if value == nil {
			return fmt.Errorf("AttributeError: can't delete element attribute")
		}
		d, ok := value.(*objects.Dict)
		if !ok {
			return fmt.Errorf("TypeError: attrib must be dict, not '%s'", typeName(value))
		}
		e.attrib = d
		return nil
	}
	return objects.GenericSetAttr(o, name, value)
}

// elementRepr formats an Element with its tag for diagnostic output:
// `<Element 'tag' at 0xHEXADDR>`. CPython prints the PyObject pointer
// directly; gopy uses the Element's runtime address so identity
// comparisons in repr survive across runs.
//
// CPython: Modules/_elementtree.c:2168 element_repr
func elementRepr(o objects.Object) (string, error) {
	e, ok := o.(*Element)
	if !ok {
		return "", fmt.Errorf("TypeError: expected Element")
	}
	tagStr, err := objects.Repr(e.tag)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("<Element %s at %p>", tagStr, e), nil
}

// subElement runs `SubElement(parent, tag, attrib={}, **extra)`:
// constructs a fresh Element and appends it to parent.children. The
// keyword folding and attrib argument parsing match Element().
//
// CPython: Modules/_elementtree.c:606 subelement
func subElement(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: SubElement() takes at least 2 positional arguments (got %d)", len(args))
	}
	if len(args) > 3 {
		return nil, fmt.Errorf("TypeError: SubElement() takes at most 3 positional arguments (got %d)", len(args))
	}
	parent, ok := args[0].(*Element)
	if !ok {
		return nil, fmt.Errorf("TypeError: expected an Element, not \"%s\"", typeName(args[0]))
	}
	childArgs := append([]objects.Object{args[1]}, args[2:]...)
	childObj, err := elementTpNew(elementType, childArgs, kwargs)
	if err != nil {
		return nil, err
	}
	child := childObj.(*Element)
	parent.children = append(parent.children, child)
	return child, nil
}

// typeName extracts the type name for a TypeError message, mirroring
// `Py_TYPE(o)->tp_name`.
func typeName(o objects.Object) string {
	if o == nil {
		return "NoneType"
	}
	if t := o.Type(); t != nil {
		return t.Name
	}
	return "object"
}
