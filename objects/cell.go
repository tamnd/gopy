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
	CellType.Getattro = GenericGetAttr
	SetTypeDescr(CellType, "cell_contents", NewGetSetDescr("cell_contents",
		func(o Object) (Object, error) {
			c := o.(*Cell)
			if c.Contents == nil {
				return nil, fmt.Errorf("ValueError: cell is empty")
			}
			return c.Contents, nil
		},
		func(o Object, v Object) error {
			c := o.(*Cell)
			if v == nil || IsNone(v) {
				c.Contents = nil
			} else {
				c.Contents = v
			}
			return nil
		},
	))
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
// CPython: Objects/cellobject.c PyCell_New
func NewCell(contents Object) *Cell {
	c := &Cell{Contents: contents}
	c.init(CellType)
	return c
}
