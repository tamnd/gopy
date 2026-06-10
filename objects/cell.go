package objects

import "fmt"

// Cell holds a single Object reference shared between an enclosing
// function and its inner closures. MAKE_CELL allocates one,
// LOAD_DEREF / STORE_DEREF read and write Contents, and
// COPY_FREE_VARS hands the outer cells to the inner frame.
//
// CPython: Include/cpython/cellobject.h PyCellObject
type Cell struct {
	Header
	Contents Object // may be nil to mean "unbound"
}

// CellType is the type singleton for cell objects.
//
// CPython: Objects/cellobject.c PyCell_Type
var CellType = NewType("cell", []*Type{objectType})

func init() {
	CellType.TpTraverse = cellTraverse
	CellType.TpNew = cellTpNew
	// CPython: Objects/cellobject.c cell_dealloc (GC variant)
	CellType.Dealloc = cellDealloc
	CellType.Getattro = GenericGetAttr
	// cell uses the default object tp_setattro (PyObject_GenericSetAttr) so
	// cell_contents assignment reaches the getset setter below; without it,
	// SetAttr reports "only read-only attributes".
	CellType.Setattro = GenericSetAttr
	CellType.RichCmp = cellRichCmp
	SetTypeDescr(CellType, "cell_contents", NewGetSetDescr("cell_contents",
		// CPython: Objects/cellobject.c:147 cell_get_contents
		func(o Object) (Object, error) {
			c := o.(*Cell)
			if c.Contents == nil {
				return nil, fmt.Errorf("ValueError: Cell is empty")
			}
			return c.Contents, nil
		},
		// CPython: Objects/cellobject.c:159 cell_set_contents
		func(o Object, v Object) error {
			c := o.(*Cell)
			old := c.Contents
			if v != nil {
				Incref(v)
			}
			c.Contents = v
			if old != nil {
				Decref(old)
			}
			return nil
		},
	))
}

// cellRichCmp compares cells by their contents. Empty cells sort before
// anything else, so two empties are equal and an empty is less than a
// bound cell.
//
// CPython: Objects/cellobject.c:97 cell_richcompare
func cellRichCmp(a, b Object, op CompareOp) (Object, error) {
	ac, ok := a.(*Cell)
	if !ok {
		return NotImplemented(), nil
	}
	bc, ok := b.(*Cell)
	if !ok {
		return NotImplemented(), nil
	}
	aref, bref := ac.Contents, bc.Contents
	// CPython: Objects/cellobject.c:84 cell_compare_impl
	if aref != nil && bref != nil {
		return RichCmp(aref, bref, op)
	}
	return boolRichCompare(bref == nil, aref == nil, op), nil
}

// boolRichCompare evaluates op on two booleans treated as 0/1, the
// Py_RETURN_RICHCOMPARE idiom for ordering sentinel flags.
//
// CPython: Include/object.h Py_RETURN_RICHCOMPARE
func boolRichCompare(x, y bool, op CompareOp) Object {
	xi, yi := 0, 0
	if x {
		xi = 1
	}
	if y {
		yi = 1
	}
	var r bool
	switch op {
	case CompareLT:
		r = xi < yi
	case CompareLE:
		r = xi <= yi
	case CompareEQ:
		r = xi == yi
	case CompareNE:
		r = xi != yi
	case CompareGT:
		r = xi > yi
	case CompareGE:
		r = xi >= yi
	}
	return NewBool(r)
}

// cellTpNew implements cell() constructor. With no argument creates an
// empty cell; with one argument creates a cell holding that value.
//
// CPython: Objects/cellobject.c:267 cell_new
func cellTpNew(_ *Type, args []Object, _ map[string]Object) (Object, error) {
	switch len(args) {
	case 0:
		return NewCell(nil), nil
	case 1:
		return NewCell(args[0]), nil
	default:
		return nil, fmt.Errorf("TypeError: cell() takes at most 1 argument (%d given)", len(args))
	}
}

// cellDealloc releases the cell's contents reference when the cell's
// refcount drops to zero.
//
// CPython: Objects/cellobject.c cell_dealloc
func cellDealloc(o Object) {
	c := o.(*Cell)
	if c.Contents != nil {
		Decref(c.Contents)
	}
}

// cellTraverse visits the contents reference. Mirrors cell_traverse.
//
// CPython: Objects/cellobject.c:282 cell_traverse
func cellTraverse(o Object, visit Visitor) error {
	c := o.(*Cell)
	if c.Contents == nil {
		return nil
	}
	return visit(c.Contents)
}

// NewCell builds a cell holding contents. Pass nil for an unbound cell.
//
// CPython: Objects/cellobject.c:16 PyCell_New
func NewCell(contents Object) *Cell {
	// CPython: Objects/cellobject.c:16 Py_XNewRef — bumps refcount so the
	// cell owns a strong reference to its initial contents.
	if contents != nil {
		Incref(contents)
	}
	c := &Cell{Contents: contents}
	c.init(CellType)
	return c
}
