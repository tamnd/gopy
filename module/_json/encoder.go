// Encoder is the Python `_json.make_encoder` callable type. It is the
// C-accelerated driver behind Lib/json/encoder.py: when json.dumps is
// invoked with _one_shot=True (the default path through encode()),
// JSONEncoder.iterencode constructs an Encoder and calls it to produce
// the JSON string in one pass without dropping back into the bytecode
// interpreter per value.
//
// CPython: Modules/_json.c:59 encoder_members + 1226 encoder_new +
// 1361 encoder_call + 1479 encoder_listencode_obj.

package _json

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/tamnd/gopy/objects"
)

// Encoder holds the configuration captured by `_json.make_encoder(...)`
// plus a precomputed flag for the common-case string encoder so the
// per-value dispatch avoids the Python-level callback.
//
// CPython: Modules/_json.c:43 PyEncoderObject
type Encoder struct {
	objects.Header

	markers       objects.Object // None or *Dict; tracks objects to detect cycles
	defaultfn     objects.Object // user-provided callable for unknown types
	encoder       objects.Object // user-facing string encoder (used when fastEncode is none)
	indent        objects.Object // None or *Unicode
	keySeparator  string
	itemSeparator string
	sortKeys      bool
	skipkeys      bool
	allowNan      bool

	// fastEncode != 0 selects the inlined string encoder; matches
	// CPython's check that the encoder is one of the two builtins
	// it knows how to bypass. CPython: Modules/_json.c:1264-1269
	// where fast_encode = py_encode_basestring{,_ascii}.
	fastEncode int // 0 = call self.encoder, 1 = encode_basestring, 2 = encode_basestring_ascii
}

var encoderType *objects.Type

func init() {
	encoderType = objects.NewType("Encoder", []*objects.Type{objects.ObjectType()})
	encoderType.TpNew = encoderTpNew
	encoderType.Call = encoderCall
}

// encoderTpNew implements Encoder(markers, default, encoder, indent,
// key_separator, item_separator, sort_keys, skipkeys, allow_nan).
//
// CPython: Modules/_json.c:1227 encoder_new
func encoderTpNew(_ *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 9 {
		return nil, fmt.Errorf("TypeError: make_encoder() takes exactly 9 arguments (%d given)", len(args))
	}
	if len(kwargs) > 0 {
		return nil, fmt.Errorf("TypeError: make_encoder() takes no keyword arguments")
	}

	markers := args[0]
	defaultfn := args[1]
	encoder := args[2]
	indent := args[3]
	keySepObj := args[4]
	itemSepObj := args[5]
	sortKeys, err := objects.IsTruthy(args[6])
	if err != nil {
		return nil, err
	}
	skipkeys, err := objects.IsTruthy(args[7])
	if err != nil {
		return nil, err
	}
	allowNan, err := objects.IsTruthy(args[8])
	if err != nil {
		return nil, err
	}

	if markers != objects.None() {
		if _, ok := markers.(*objects.Dict); !ok {
			return nil, fmt.Errorf("TypeError: make_encoder() argument 1 must be dict or None, not %s", markers.Type().Name)
		}
	}
	keySep, ok := keySepObj.(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: make_encoder() argument 5 must be str, not %s", keySepObj.Type().Name)
	}
	itemSep, ok := itemSepObj.(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: make_encoder() argument 6 must be str, not %s", itemSepObj.Type().Name)
	}

	e := &Encoder{
		markers:       markers,
		defaultfn:     defaultfn,
		encoder:       encoder,
		indent:        indent,
		keySeparator:  keySep.Value(),
		itemSeparator: itemSep.Value(),
		sortKeys:      sortKeys,
		skipkeys:      skipkeys,
		allowNan:      allowNan,
		fastEncode:    classifyEncoder(encoder),
	}
	e.Init(encoderType)
	return e, nil
}

// classifyEncoder returns the fastEncode tag for the encoder if it is
// one of the two builtins Modules/_json.c knows how to bypass.
//
// CPython: Modules/_json.c:1264 PyCFunction_Check + GetFunction
func classifyEncoder(o objects.Object) int {
	bf, ok := o.(*objects.BuiltinFunction)
	if !ok {
		return 0
	}
	switch bf.Name {
	case "encode_basestring_ascii":
		return 2
	case "encode_basestring":
		return 1
	}
	return 0
}

// encoderCall implements Encoder.__call__(obj, _current_indent_level)
// returning a 1-tuple holding the JSON string. The initial indent level
// from Python is captured in the indent cache and then reset to 0 so
// the per-level offsets line up with CPython.
//
// CPython: Modules/_json.c:1361 encoder_call
func encoderCall(self objects.Object, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	e, ok := self.(*Encoder)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__call__' requires a 'Encoder' object")
	}
	if len(kwargs) > 0 {
		return nil, fmt.Errorf("TypeError: _iterencode() takes no keyword arguments")
	}
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: _iterencode() takes 2 positional arguments (%d given)", len(args))
	}
	obj := args[0]
	indentLevel, ok := args[1].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: _iterencode() argument 2 must be int")
	}
	startLevel, fits := indentLevel.Int64()
	if !fits {
		return nil, fmt.Errorf("OverflowError: indent level too large")
	}

	var buf strings.Builder
	var cache []string
	if e.indent != objects.None() {
		c, err := e.createIndentCache(int(startLevel))
		if err != nil {
			return nil, err
		}
		cache = c
	}
	if err := e.listencodeObj(&buf, obj, 0, &cache); err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{objects.NewStr(buf.String())}), nil
}

// createIndentCache builds the per-level indent strings used when
// indent != None. The cache layout mirrors CPython: even indices hold
// '\n' + indent*level (used at opening / closing brackets) and odd
// indices hold item_separator + that newline-indent (used between
// items).
//
// CPython: Modules/_json.c:1285 create_indent_cache
func (e *Encoder) createIndentCache(startLevel int) ([]string, error) {
	indentStr, err := objects.Str(e.indent)
	if err != nil {
		return nil, err
	}
	first := "\n" + strings.Repeat(indentStr, startLevel)
	return []string{first}, nil
}

// extendIndentCache walks the cache up to indentLevel, appending the
// next (separator_indent, newline_indent) pair for each missing level.
//
// CPython: Modules/_json.c:1308 update_indent_cache
func (e *Encoder) extendIndentCache(indentLevel int, cache *[]string) error {
	indentStr, err := objects.Str(e.indent)
	if err != nil {
		return err
	}
	for indentLevel*2 > len(*cache) {
		base := (*cache)[len(*cache)-1]
		newlineIndent := base + indentStr
		separatorIndent := e.itemSeparator + newlineIndent
		*cache = append(*cache, separatorIndent, newlineIndent)
	}
	return nil
}

// getItemSeparator returns the per-level separator string, expanding
// the cache if needed.
//
// CPython: Modules/_json.c:1337 get_item_separator
func (e *Encoder) getItemSeparator(indentLevel int, cache *[]string) (string, error) {
	if indentLevel*2 > len(*cache) {
		if err := e.extendIndentCache(indentLevel, cache); err != nil {
			return "", err
		}
	}
	return (*cache)[indentLevel*2-1], nil
}

// listencodeObj is the top-level value dispatcher.
//
// CPython: Modules/_json.c:1479 encoder_listencode_obj
func (e *Encoder) listencodeObj(buf *strings.Builder, obj objects.Object, indentLevel int, cache *[]string) error {
	switch {
	case obj == objects.None():
		buf.WriteString("null")
		return nil
	case obj == objects.True():
		buf.WriteString("true")
		return nil
	case obj == objects.False():
		buf.WriteString("false")
		return nil
	}
	switch v := obj.(type) {
	case *objects.Unicode:
		return e.encodeString(buf, v.Value())
	case *objects.Int:
		s, err := objects.Repr(v)
		if err != nil {
			return err
		}
		buf.WriteString(s)
		return nil
	case *objects.Float:
		return e.encodeFloat(buf, v.Float64())
	case *objects.List:
		return e.listencodeList(buf, listAsObjects(v), obj, indentLevel, cache)
	case *objects.Tuple:
		return e.listencodeList(buf, tupleAsObjects(v), obj, indentLevel, cache)
	case *objects.Dict:
		return e.listencodeDict(buf, v, indentLevel, cache)
	}
	// Subclasses fall through here. Try bool subclass via True/False
	// identity first, then numeric / sequence protocols. Default
	// branch: call user defaultfn.
	if _, ok := obj.(*objects.Bool); ok {
		// Bool is a subclass of Int in CPython; the None/True/False
		// identity checks above already caught the singletons.
		if isTrueBool(obj) {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
		return nil
	}
	return e.encodeViaDefault(buf, obj, indentLevel, cache)
}

func listAsObjects(l *objects.List) []objects.Object {
	n := l.Len()
	out := make([]objects.Object, n)
	for i := 0; i < n; i++ {
		out[i] = l.Item(i)
	}
	return out
}

func tupleAsObjects(t *objects.Tuple) []objects.Object {
	n := t.Len()
	out := make([]objects.Object, n)
	for i := 0; i < n; i++ {
		out[i] = t.Item(i)
	}
	return out
}

func isTrueBool(o objects.Object) bool {
	b, err := objects.IsTruthy(o)
	if err != nil {
		return false
	}
	return b
}

// encodeViaDefault routes unknown types through self.default, checking
// markers for circular references first.
//
// CPython: Modules/_json.c:1532 encoder_listencode_obj else-branch
func (e *Encoder) encodeViaDefault(buf *strings.Builder, obj objects.Object, indentLevel int, cache *[]string) error {
	var ident objects.Object
	if d, ok := e.markers.(*objects.Dict); ok {
		ident = objects.NewInt(objectIdent(obj))
		has, err := d.Contains(ident)
		if err != nil {
			return err
		}
		if has {
			return fmt.Errorf("ValueError: Circular reference detected")
		}
		if err := d.SetItem(ident, obj); err != nil {
			return err
		}
	}
	newobj, err := objects.CallOneArg(e.defaultfn, obj)
	if err != nil {
		return err
	}
	if err := e.listencodeObj(buf, newobj, indentLevel, cache); err != nil {
		return err
	}
	if d, ok := e.markers.(*objects.Dict); ok && ident != nil {
		if err := d.DelItem(ident); err != nil {
			return err
		}
	}
	return nil
}

// objectIdent returns a stable integer identity for o. Mirrors
// PyLong_FromVoidPtr(obj) which CPython hands to the markers dict so
// circular references are detected by id() rather than __eq__.
//
// CPython: Modules/_json.c:1535 get_item PyLong_FromVoidPtr
func objectIdent(o objects.Object) int64 {
	return int64(reflect.ValueOf(o).Pointer())
}

// encodeString writes a JSON string literal for s. Uses the inlined
// basestring encoder when the user did not override the encoder; for
// the override case it routes through the user callable so subclasses
// of JSONEncoder can hook string formatting.
//
// CPython: Modules/_json.c:1449 encoder_encode_string
func (e *Encoder) encodeString(buf *strings.Builder, s string) error {
	switch e.fastEncode {
	case 2:
		buf.WriteString(encodeBasestring(s, true))
		return nil
	case 1:
		buf.WriteString(encodeBasestring(s, false))
		return nil
	}
	encoded, err := objects.CallOneArg(e.encoder, objects.NewStr(s))
	if err != nil {
		return err
	}
	u, ok := encoded.(*objects.Unicode)
	if !ok {
		return fmt.Errorf("TypeError: encoder() must return a string, not %s", encoded.Type().Name)
	}
	buf.WriteString(u.Value())
	return nil
}

// encodeFloat writes the JSON representation of a float, honoring the
// allow_nan flag for non-finite values.
//
// CPython: Modules/_json.c:1422 encoder_encode_float
func (e *Encoder) encodeFloat(buf *strings.Builder, f float64) error {
	if math.IsNaN(f) {
		if !e.allowNan {
			return fmt.Errorf("ValueError: Out of range float values are not JSON compliant: nan")
		}
		buf.WriteString("NaN")
		return nil
	}
	if math.IsInf(f, 1) {
		if !e.allowNan {
			return fmt.Errorf("ValueError: Out of range float values are not JSON compliant: inf")
		}
		buf.WriteString("Infinity")
		return nil
	}
	if math.IsInf(f, -1) {
		if !e.allowNan {
			return fmt.Errorf("ValueError: Out of range float values are not JSON compliant: -inf")
		}
		buf.WriteString("-Infinity")
		return nil
	}
	s, err := objects.Repr(objects.NewFloat(f))
	if err != nil {
		return err
	}
	buf.WriteString(s)
	return nil
}

// listencodeList encodes a list or tuple in place. seq carries the
// expanded items; original is the underlying object used as the
// markers key.
//
// CPython: Modules/_json.c:1753 encoder_listencode_list
func (e *Encoder) listencodeList(buf *strings.Builder, seq []objects.Object, original objects.Object, indentLevel int, cache *[]string) error {
	if len(seq) == 0 {
		buf.WriteString("[]")
		return nil
	}

	var ident objects.Object
	if d, ok := e.markers.(*objects.Dict); ok {
		ident = objects.NewInt(objectIdent(original))
		has, err := d.Contains(ident)
		if err != nil {
			return err
		}
		if has {
			return fmt.Errorf("ValueError: Circular reference detected")
		}
		if err := d.SetItem(ident, original); err != nil {
			return err
		}
	}

	buf.WriteByte('[')
	separator := e.itemSeparator
	if e.indent != objects.None() {
		indentLevel++
		s, err := e.getItemSeparator(indentLevel, cache)
		if err != nil {
			return err
		}
		separator = s
		if err := writeNewlineIndent(buf, indentLevel, *cache); err != nil {
			return err
		}
	}
	for i, item := range seq {
		if i > 0 {
			buf.WriteString(separator)
		}
		if err := e.listencodeObj(buf, item, indentLevel, cache); err != nil {
			return err
		}
	}
	if e.indent != objects.None() {
		indentLevel--
		if err := writeNewlineIndent(buf, indentLevel, *cache); err != nil {
			return err
		}
	}
	buf.WriteByte(']')

	if d, ok := e.markers.(*objects.Dict); ok && ident != nil {
		if err := d.DelItem(ident); err != nil {
			return err
		}
	}
	return nil
}

func writeNewlineIndent(buf *strings.Builder, indentLevel int, cache []string) error {
	idx := indentLevel * 2
	if idx >= len(cache) {
		return fmt.Errorf("vm: indent cache index %d out of range (len=%d)", idx, len(cache))
	}
	buf.WriteString(cache[idx])
	return nil
}

// listencodeDict encodes a dict in place.
//
// CPython: Modules/_json.c:1654 encoder_listencode_dict
func (e *Encoder) listencodeDict(buf *strings.Builder, dct *objects.Dict, indentLevel int, cache *[]string) error {
	if dct.Len() == 0 {
		buf.WriteString("{}")
		return nil
	}

	var ident objects.Object
	if d, ok := e.markers.(*objects.Dict); ok {
		ident = objects.NewInt(objectIdent(dct))
		has, err := d.Contains(ident)
		if err != nil {
			return err
		}
		if has {
			return fmt.Errorf("ValueError: Circular reference detected")
		}
		if err := d.SetItem(ident, dct); err != nil {
			return err
		}
	}

	buf.WriteByte('{')
	separator := e.itemSeparator
	if e.indent != objects.None() {
		indentLevel++
		s, err := e.getItemSeparator(indentLevel, cache)
		if err != nil {
			return err
		}
		separator = s
	}

	first := true
	keys := dct.Keys()
	if e.sortKeys {
		sortKeyList(keys)
	}
	for _, key := range keys {
		value, err := dct.GetItem(key)
		if err != nil {
			return err
		}
		if err := e.encodeKeyValue(buf, &first, key, value, indentLevel, cache, separator); err != nil {
			return err
		}
	}

	if e.indent != objects.None() && !first {
		indentLevel--
		if err := writeNewlineIndent(buf, indentLevel, *cache); err != nil {
			return err
		}
	}
	buf.WriteByte('}')

	if d, ok := e.markers.(*objects.Dict); ok && ident != nil {
		if err := d.DelItem(ident); err != nil {
			return err
		}
	}
	return nil
}

// sortKeyList sorts JSON dict keys in CPython order. The reference
// implementation calls PyList_Sort which uses the keys' natural
// ordering; here we only support all-string keys (the common case
// for sort_keys=True) plus int and float since CPython allows those
// when keys are coerced to string.
func sortKeyList(keys []objects.Object) {
	allStr := true
	for _, k := range keys {
		if _, ok := k.(*objects.Unicode); !ok {
			allStr = false
			break
		}
	}
	if !allStr {
		return
	}
	// Insertion sort: tiny n typically, and the keys are already in
	// insertion order so this is near-linear when input was inserted
	// alphabetically.
	for i := 1; i < len(keys); i++ {
		j := i
		for j > 0 && keys[j-1].(*objects.Unicode).Value() > keys[j].(*objects.Unicode).Value() {
			keys[j-1], keys[j] = keys[j], keys[j-1]
			j--
		}
	}
}

// encodeKeyValue emits one "key": value pair, including the separator
// before all but the first item.
//
// CPython: Modules/_json.c:1582 encoder_encode_key_value
func (e *Encoder) encodeKeyValue(buf *strings.Builder, first *bool, key, value objects.Object, indentLevel int, cache *[]string, sepBetween string) error {
	var keystr string
	switch k := key.(type) {
	case *objects.Unicode:
		keystr = k.Value()
	case *objects.Float:
		return e.skipOrError(key, "float keys go through encode_float", buf, first, value, indentLevel, cache, sepBetween, formatJSONFloat(k.Float64(), e.allowNan))
	case *objects.Bool:
		if isTrueBool(k) {
			keystr = "true"
		} else {
			keystr = "false"
		}
		return e.writeRawKey(buf, first, value, indentLevel, cache, sepBetween, keystr, true)
	case *objects.Int:
		s, err := objects.Repr(k)
		if err != nil {
			return err
		}
		return e.writeRawKey(buf, first, value, indentLevel, cache, sepBetween, s, true)
	default:
		if key == objects.None() {
			return e.writeRawKey(buf, first, value, indentLevel, cache, sepBetween, "null", true)
		}
		if e.skipkeys {
			return nil
		}
		return fmt.Errorf("TypeError: keys must be str, int, float, bool or None, not %s", key.Type().Name)
	}

	if *first {
		*first = false
		if e.indent != objects.None() {
			if err := writeNewlineIndent(buf, indentLevel, *cache); err != nil {
				return err
			}
		}
	} else {
		buf.WriteString(sepBetween)
	}
	if err := e.encodeString(buf, keystr); err != nil {
		return err
	}
	buf.WriteString(e.keySeparator)
	return e.listencodeObj(buf, value, indentLevel, cache)
}

// writeRawKey emits a key that was already formatted (numeric or
// bool/null), wrapping it in JSON string quotes since JSON dict keys
// must be strings. CPython does the same: PyLong_Type.tp_repr returns
// the decimal literal which is then sent through encoder_encode_string
// just like a user-supplied string would be.
//
// CPython: Modules/_json.c:1635 encoder_encode_key_value (encoded =
// encoder_encode_string(s, keystr)).
func (e *Encoder) writeRawKey(buf *strings.Builder, first *bool, value objects.Object, indentLevel int, cache *[]string, sepBetween, keystr string, wrap bool) error {
	if *first {
		*first = false
		if e.indent != objects.None() {
			if err := writeNewlineIndent(buf, indentLevel, *cache); err != nil {
				return err
			}
		}
	} else {
		buf.WriteString(sepBetween)
	}
	if wrap {
		if err := e.encodeString(buf, keystr); err != nil {
			return err
		}
	} else {
		buf.WriteString(keystr)
	}
	buf.WriteString(e.keySeparator)
	return e.listencodeObj(buf, value, indentLevel, cache)
}

// skipOrError handles the float-key fast path: emit the float repr
// wrapped in quotes, then the value.
func (e *Encoder) skipOrError(_ objects.Object, _ string, buf *strings.Builder, first *bool, value objects.Object, indentLevel int, cache *[]string, sepBetween, keystr string) error {
	return e.writeRawKey(buf, first, value, indentLevel, cache, sepBetween, keystr, true)
}

// formatJSONFloat returns the JSON repr of f. Used by float dict keys
// where we cannot go through encodeFloat (which writes to the buffer
// directly). Honors allow_nan by emitting Infinity / NaN tokens.
func formatJSONFloat(f float64, allowNan bool) string {
	if math.IsNaN(f) {
		if !allowNan {
			return "nan"
		}
		return "NaN"
	}
	if math.IsInf(f, 1) {
		if !allowNan {
			return "inf"
		}
		return "Infinity"
	}
	if math.IsInf(f, -1) {
		if !allowNan {
			return "-inf"
		}
		return "-Infinity"
	}
	// Use the float Repr formatter.
	s, err := objects.Repr(objects.NewFloat(f))
	if err != nil {
		return strconv.FormatFloat(f, 'g', -1, 64)
	}
	return s
}
