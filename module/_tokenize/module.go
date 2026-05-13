// Package _tokenize is a placeholder port of CPython's
// Modules/_tokenize.c. The full module exposes a TokenizerIter type
// backed by Parser/tokenizer.c; vendoring that lives behind its own
// spec row. For now the module registers itself in the inittab and
// exposes a TokenizerIter constructor that raises NotImplementedError
// when invoked, so `import tokenize` (and the chain `import
// codeop` -> `import traceback`) can complete at module load.
//
// CPython: Modules/_tokenize.c

package _tokenize

import (
	"fmt"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_tokenize", buildModule)
}

// tokenizerIterType is a stub TokenizerIter that raises when called.
// CPython exposes the full iterator via tp_iter / tp_next; that work
// lives in the dedicated _tokenize port.
//
// CPython: Modules/_tokenize.c:53 TokenizerIter_Type
var tokenizerIterType = objects.NewType("TokenizerIter", []*objects.Type{objects.ObjectType()})

func init() {
	tokenizerIterType.Call = func(_ objects.Object, _ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return nil, fmt.Errorf("NotImplementedError: _tokenize.TokenizerIter is not yet ported; tokenize() needs Parser/tokenizer.c")
	}
}

// buildModule materializes the _tokenize module dict.
//
// CPython: Modules/_tokenize.c:281 _tokenize_module
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_tokenize")
	d := m.Dict()
	if err := d.SetItem(objects.NewStr("TokenizerIter"), tokenizerIterType); err != nil {
		return nil, err
	}
	return m, nil
}
