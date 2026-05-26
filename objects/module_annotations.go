// PEP 649 / 749 lazy __annotations__ and __annotate__ on module objects.
// Mirrors the module_get_annotate / module_set_annotate /
// module_get_annotations / module_set_annotations getset slots.
//
// The module's namespace (m.dict) is the storage. The body bytecode
// installs __annotate__ via STORE_NAME at end-of-module when the body
// had annotations; first access to __annotations__ calls it with
// format=VALUE and caches the result back in the dict.
//
// CPython: Objects/moduleobject.c:1233 module_get_annotate
// CPython: Objects/moduleobject.c:1254 module_set_annotate
// CPython: Objects/moduleobject.c:1288 module_get_annotations
// CPython: Objects/moduleobject.c:1359 module_set_annotations

package objects

import "fmt"

// moduleGetAnnotate returns __annotate__ from the module dict, or None
// when absent. The CPython form lazily seeds the dict with None on
// first read; gopy mirrors that so subsequent writes via
// moduleSetAnnotate observe a present slot.
//
// CPython: Objects/moduleobject.c:1233 module_get_annotate
func moduleGetAnnotate(m *Module) (Object, error) {
	ann, err := m.dict.GetItem(NewStr("__annotate__"))
	if err == nil {
		return ann, nil
	}
	none := None()
	if err := m.dict.SetItem(NewStr("__annotate__"), none); err != nil {
		return nil, err
	}
	return none, nil
}

// moduleSetAnnotate stores __annotate__ on the module dict, validating
// that the value is callable or None. Deletion is rejected. Setting a
// non-None callable clears any cached __annotations__ so the next read
// invokes the new annotate.
//
// CPython: Objects/moduleobject.c:1254 module_set_annotate
func moduleSetAnnotate(m *Module, value Object) error {
	if value == nil {
		return fmt.Errorf("TypeError: cannot delete __annotate__ attribute")
	}
	if !IsNone(value) && !Callable(value) {
		return fmt.Errorf("TypeError: __annotate__ must be callable or None")
	}
	if err := m.dict.SetItem(NewStr("__annotate__"), value); err != nil {
		return err
	}
	if !IsNone(value) {
		_ = m.dict.DelItem(NewStr("__annotations__"))
	}
	return nil
}

// moduleGetAnnotations returns __annotations__ from the module dict,
// materializing it via __annotate__(VALUE) on first miss. The result is
// cached back into the dict unless the module is still initializing
// (md_dict carries an __spec__ whose _initializing flag is set).
//
// CPython: Objects/moduleobject.c:1288 module_get_annotations
func moduleGetAnnotations(m *Module) (Object, error) {
	ann, err := m.dict.GetItem(NewStr("__annotations__"))
	if err == nil {
		return ann, nil
	}
	annotate, aerr := m.dict.GetItem(NewStr("__annotate__"))
	var out Object
	if aerr == nil && Callable(annotate) {
		v, cerr := Call(annotate, NewTuple([]Object{NewInt(1)}), nil)
		if cerr != nil {
			return nil, cerr
		}
		d, ok := v.(*Dict)
		if !ok {
			return nil, fmt.Errorf("TypeError: __annotate__ returned non-dict of type '%s'", v.Type().Name)
		}
		out = d
	} else {
		out = NewDict()
	}
	// Do not cache while the module is still initializing. Circular
	// imports may query __annotations__ before the body has finished;
	// caching at that point would freeze a partial result.
	//
	// CPython: Objects/moduleobject.c:1340 !is_initializing
	if !m.Initializing {
		if err := m.dict.SetItem(NewStr("__annotations__"), out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// moduleSetAnnotations stores __annotations__ on the module dict, or
// deletes it when value==nil. Either path also drops __annotate__ so a
// stale annotate function never overrides an explicit user write.
//
// CPython: Objects/moduleobject.c:1359 module_set_annotations
func moduleSetAnnotations(m *Module, value Object) error {
	if value != nil {
		if err := m.dict.SetItem(NewStr("__annotations__"), value); err != nil {
			return err
		}
	} else {
		if _, err := m.dict.GetItem(NewStr("__annotations__")); err != nil {
			return fmt.Errorf("AttributeError: __annotations__")
		}
		if err := m.dict.DelItem(NewStr("__annotations__")); err != nil {
			return err
		}
	}
	_ = m.dict.DelItem(NewStr("__annotate__"))
	return nil
}
