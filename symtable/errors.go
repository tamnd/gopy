package symtable

import (
	"fmt"

	"github.com/tamnd/gopy/ast"
)

// SyntaxError is the build-time error raised by the symtable visit
// and analyze passes. The Msg shape mirrors CPython's SyntaxError
// strings line-for-line so existing test suites and IDE diagnostics
// match. The future package follows the same pattern.
//
// CPython: Python/symtable.c uses PyErr_Format/PyErr_SetString with
// PyExc_SyntaxError plus PyErr_RangedSyntaxLocationObject.
type SyntaxError struct {
	Msg      string
	Filename string
	Pos      ast.Pos
}

// Error implements the error interface. The "SyntaxError:" prefix lets
// the VM's prefix-based unwind table promote bare symtable errors to a
// real SyntaxError instance, the same effect CPython gets for free via
// PyErr_SetObject(PyExc_SyntaxError, ...).
//
// CPython: Python/symtable.c PyErr_Format(PyExc_SyntaxError, ...) message body
func (e *SyntaxError) Error() string { return "SyntaxError: " + e.Msg }

// Error message formats. Each constant is the printf template from
// symtable.c with `%U` (PyObject) replaced by `%s`. Wrappers below
// fill them in.
//
// CPython: Python/symtable.c:L16
const (
	msgGlobalParam        = "name '%s' is parameter and global"
	msgNonlocalParam      = "name '%s' is parameter and nonlocal"
	msgGlobalAfterAssign  = "name '%s' is assigned to before global declaration"
	msgNonlocalAfterAss   = "name '%s' is assigned to before nonlocal declaration"
	msgGlobalAfterUse     = "name '%s' is used prior to global declaration"
	msgNonlocalAfterUse   = "name '%s' is used prior to nonlocal declaration"
	msgGlobalAnnot        = "annotated name '%s' can't be global"
	msgNonlocalAnnot      = "annotated name '%s' can't be nonlocal"
	msgImportStar         = "import * only allowed at module level"
	msgNamedExprComp      = "assignment expression within a comprehension cannot be used in a class body"
	msgNamedExprBound     = "assignment expression within a comprehension cannot be used in a TypeVar bound"
	msgNamedExprAlias     = "assignment expression within a comprehension cannot be used in a type alias"
	msgNamedExprParam     = "assignment expression within a comprehension cannot be used within the definition of a generic"
	msgNamedExprConflict  = "assignment expression cannot rebind comprehension iteration variable '%s'"
	msgNamedExprInner     = "comprehension inner loop cannot rebind assignment expression target '%s'"
	msgNamedExprIterExpr  = "assignment expression cannot be used in a comprehension iterable expression"
	msgAnnotationNotAllow = "%s cannot be used within an annotation"
	msgNotAllowedInTypVar = "%s cannot be used within %s"
	msgNotAllowedInAlias  = "%s cannot be used within a type alias"
	msgNotAllowedInParams = "%s cannot be used within the definition of a generic"
	msgDupTypeParam       = "duplicate type parameter '%s'"
	msgDupArgument        = "duplicate argument '%s' in function definition"
	msgAsyncWithOutside   = "'async with' outside async function"
	msgAsyncForOutside    = "'async for' outside async function"
	msgAsyncCompOutside   = "asynchronous comprehension outside of an asynchronous function"
	msgAwaitOutsideFunc   = "'await' outside function"
	msgAwaitOutsideAsync  = "'await' outside async function"
	msgAssignDebug        = "cannot assign to __debug__"
	msgDeleteDebug        = "cannot delete __debug__"
	msgFutureLate         = "from __future__ imports must occur at the beginning of the file"
	msgNonlocalAtModule   = "nonlocal declaration not allowed at module level"
	msgNoBindingNonlocal  = "no binding for nonlocal '%s' found"
	msgNonlocalGlobal     = "name '%s' is nonlocal and global"
	msgNonlocalTypeParam  = "nonlocal binding not allowed for type parameter '%s'"
)

// errorf builds a *SyntaxError with the given location.
//
// CPython: Python/symtable.c PyErr_Format + PyErr_RangedSyntaxLocationObject
func errorf(filename string, loc ast.Pos, format string, args ...any) *SyntaxError {
	return &SyntaxError{
		Msg:      fmt.Sprintf(format, args...),
		Filename: filename,
		Pos:      loc,
	}
}
