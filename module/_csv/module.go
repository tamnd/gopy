// Package _csv is the gopy port of CPython's _csv built-in module.
// It exposes the CSV reader/writer infrastructure backed by Go's
// encoding/csv package. Dialect management, quoting constants, and the
// reader/writer types mirror the CPython surface defined in
// Modules/_csv.c.
//
// CPython: Modules/_csv.c:1 _csv module
package _csv

import (
	"errors"
	"fmt"
	"sync"

	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

// csvErrorType holds the _csv.Error class, set during buildModule and used
// by csvErr to raise typed csv.Error instances.
//
// CPython: Modules/_csv.c:1820 csv_module_exec (error_obj)
var csvErrorType *objects.Type

// csvErr creates a csv.Error Python exception wrapped as a Go error.
// Replaces fmt.Errorf("csv.Error: ...") so the raised type is csv.Error,
// not a bare RuntimeError.
//
// CPython: Modules/_csv.c PyErr_SetString(error_obj, ...)
func csvErr(msg string) error {
	if csvErrorType == nil {
		return fmt.Errorf("csv.Error: %s", msg)
	}
	exc := pyerrors.New(csvErrorType, objects.NewTuple([]objects.Object{objects.NewStr(msg)}))
	return objects.NewRaisedError(exc, msg)
}

func init() {
	_ = imp.AppendInittab("_csv", buildModule)
}

// ---------------------------------------------------------------------------
// Quoting constants.
// CPython: Modules/_csv.c:49 quoting_styles
// ---------------------------------------------------------------------------

const (
	quoteMinimal    = 0
	quoteAll        = 1
	quoteNonNumeric = 2
	quoteNone       = 3
	quoteStrings    = 4
	quoteNotNull    = 5
)

// defaultFieldSizeLimit mirrors FIELD_SIZE_LIMIT in _csv.c.
// CPython: Modules/_csv.c:66 field_limit
const defaultFieldSizeLimit = 131072

var fieldSizeLimit = defaultFieldSizeLimit

// ---------------------------------------------------------------------------
// Dialect
// ---------------------------------------------------------------------------

// Dialect holds the formatting parameters for a CSV reader or writer.
// CPython: Modules/_csv.c:106 DialectObj
type Dialect struct {
	objects.Header

	delimiter        rune
	doublequote      bool
	escapechar       rune
	hasEscapechar    bool
	lineterminator   string
	quotechar        rune
	hasQuotechar     bool
	quoting          int
	skipinitialspace bool
	strict           bool
}

// dialectType is the Python-visible Dialect type.
// Initialized in init() to break the initialization cycle with newDialectDefault.
var dialectType *objects.Type

func init() {
	// CPython: Modules/_csv.c:680 Dialect_Type
	t := objects.NewType("Dialect", []*objects.Type{objects.ObjectType()})
	t.HasDict = false
	t.Getattro = dialectGetattr
	t.TpNew = func(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
		d := newDialectDefault()
		if err := applyFmtParams(d, kwargs); err != nil {
			return nil, err
		}
		return d, nil
	}
	dialectType = t
}

// newDialectDefault returns a Dialect with Excel-compatible defaults.
//
// CPython: Modules/_csv.c:549 dialect_new
func newDialectDefault() *Dialect {
	d := &Dialect{
		delimiter:      ',',
		doublequote:    true,
		lineterminator: "\r\n",
		quotechar:      '"',
		hasQuotechar:   true,
		quoting:        quoteMinimal,
	}
	d.Init(dialectType)
	return d
}

// dialectGetattr exposes Dialect attributes to Python.
//
// CPython: Modules/_csv.c:627 Dialect_getattro
func dialectGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	d, ok := o.(*Dialect)
	if !ok {
		return nil, fmt.Errorf("TypeError: expected Dialect")
	}
	n, ok2 := name.(*objects.Unicode)
	if !ok2 {
		return nil, fmt.Errorf("TypeError: attribute name must be str")
	}
	switch n.Value() {
	case "delimiter":
		return objects.NewStr(string(d.delimiter)), nil
	case "doublequote":
		return objects.NewBool(d.doublequote), nil
	case "escapechar":
		if !d.hasEscapechar {
			return objects.None(), nil
		}
		return objects.NewStr(string(d.escapechar)), nil
	case "lineterminator":
		return objects.NewStr(d.lineterminator), nil
	case "quotechar":
		if !d.hasQuotechar {
			return objects.None(), nil
		}
		return objects.NewStr(string(d.quotechar)), nil
	case "quoting":
		return objects.NewInt(int64(d.quoting)), nil
	case "skipinitialspace":
		return objects.NewBool(d.skipinitialspace), nil
	case "strict":
		return objects.NewBool(d.strict), nil
	}
	return nil, fmt.Errorf("AttributeError: Dialect has no attribute %q", n.Value())
}

// applyFmtParams applies format parameters from kwargs onto d.
//
// CPython: Modules/_csv.c:470 dialect_check_quoting
func applyFmtParams(d *Dialect, kwargs map[string]objects.Object) error {
	if v, ok := kwargs["delimiter"]; ok {
		s, err := objToRune(v, "delimiter")
		if err != nil {
			return err
		}
		d.delimiter = s
	}
	if v, ok := kwargs["doublequote"]; ok {
		d.doublequote = objects.IsTrue(v)
	}
	if v, ok := kwargs["escapechar"]; ok {
		if v == objects.None() {
			d.hasEscapechar = false
		} else {
			r, err := objToRune(v, "escapechar")
			if err != nil {
				return err
			}
			d.escapechar = r
			d.hasEscapechar = true
		}
	}
	if v, ok := kwargs["lineterminator"]; ok {
		s, ok2 := v.(*objects.Unicode)
		if !ok2 {
			return fmt.Errorf("TypeError: lineterminator must be str")
		}
		d.lineterminator = s.Value()
	}
	if v, ok := kwargs["quotechar"]; ok {
		if v == objects.None() {
			d.hasQuotechar = false
		} else {
			r, err := objToRune(v, "quotechar")
			if err != nil {
				return err
			}
			d.quotechar = r
			d.hasQuotechar = true
		}
	}
	if v, ok := kwargs["quoting"]; ok {
		iv, ok2 := v.(*objects.Int)
		if !ok2 {
			return fmt.Errorf("TypeError: quoting must be int")
		}
		q, _ := iv.Int64()
		if q < quoteMinimal || q > quoteNotNull {
			return csvErr("bad quoting style")
		}
		d.quoting = int(q)
	}
	if v, ok := kwargs["skipinitialspace"]; ok {
		d.skipinitialspace = objects.IsTrue(v)
	}
	if v, ok := kwargs["strict"]; ok {
		d.strict = objects.IsTrue(v)
	}
	return nil
}

// objToRune expects a single-character str.
func objToRune(o objects.Object, field string) (rune, error) {
	s, ok := o.(*objects.Unicode)
	if !ok {
		return 0, fmt.Errorf("TypeError: %q must be a 1-character str", field)
	}
	rs := []rune(s.Value())
	if len(rs) != 1 {
		return 0, fmt.Errorf("TypeError: %q must be a 1-character str, not %d chars", field, len(rs))
	}
	return rs[0], nil
}

// ---------------------------------------------------------------------------
// Dialect registry
// ---------------------------------------------------------------------------

var (
	dialectMu       sync.RWMutex
	dialectRegistry = map[string]*Dialect{}
)

func init() {
	// Register built-in dialects.
	// CPython: Modules/_csv.c:1423 _csv_exec
	excel := newDialectDefault()
	dialectRegistry["excel"] = excel

	tab := newDialectDefault()
	tab.delimiter = '\t'
	tab.lineterminator = "\r\n"
	dialectRegistry["excel-tab"] = tab
}

// lookupDialect returns the named dialect or an error.
//
// CPython: Modules/_csv.c:209 get_dialect_from_registry
func lookupDialect(name string) (*Dialect, error) {
	dialectMu.RLock()
	d, ok := dialectRegistry[name]
	dialectMu.RUnlock()
	if !ok {
		return nil, csvErr(fmt.Sprintf("unknown dialect %q", name))
	}
	return d, nil
}

// ---------------------------------------------------------------------------
// Reader
// ---------------------------------------------------------------------------

// Reader is the Python csv.reader object. Each instance carries its
// own state-machine parser whose layout mirrors CPython's ReaderObj.
//
// CPython: Modules/_csv.c:795 ReaderObj
type Reader struct {
	objects.Header

	dialect *Dialect
	source  objects.Object // iterable of str
	it      objects.Object // active iterator
	lineNum int64
	parser  readerState
}

// readerType is the Python type for csv.reader objects.
var readerType = newReaderType()

func newReaderType() *objects.Type {
	t := objects.NewType("reader", []*objects.Type{objects.ObjectType()})
	t.Iter = func(o objects.Object) (objects.Object, error) { return o, nil }
	t.IterNext = readerIterNext
	t.Getattro = readerGetattr
	objects.AddIterSlotWrappers(t)
	return t
}

// readerIterNext yields the next parsed row as a list. The loop keeps
// pulling lines from the source iterator until the state machine
// returns to psStartRecord, so a quoted field that spans multiple
// input lines is folded back into a single record.
//
// CPython: Modules/_csv.c:924 Reader_iternext_lock_held
func readerIterNext(o objects.Object) (objects.Object, error) {
	r, ok := o.(*Reader)
	if !ok {
		return nil, fmt.Errorf("TypeError: expected csv.reader")
	}
	r.parser.resetParser()
	for {
		line, err := objects.IterNext(r.it)
		if err != nil {
			if !errors.Is(err, objects.ErrStopIteration) {
				return nil, err
			}
			// End of input. CPython's `Reader_iternext_lock_held` keeps the
			// partial field iff we are mid-field or mid-quoted-field.
			if len(r.parser.field) != 0 || r.parser.state == psInQuotedField {
				if r.dialect.strict {
					return nil, csvErr("unexpected end of data")
				}
				if err := r.parser.saveField(r.dialect); err != nil {
					return nil, err
				}
				break
			}
			return nil, objects.ErrStopIteration
		}
		lineStr, ok := line.(*objects.Unicode)
		if !ok {
			return nil, csvErr(fmt.Sprintf("iterator should return strings, not %s (the file should be opened in text mode)", line.Type().Name))
		}
		r.lineNum++
		for _, c := range lineStr.Value() {
			if err := r.parser.processChar(r.dialect, c); err != nil {
				return nil, err
			}
		}
		if err := r.parser.processChar(r.dialect, eol); err != nil {
			return nil, err
		}
		if r.parser.state == psStartRecord {
			break
		}
	}
	fields := r.parser.fields
	r.parser.fields = nil
	return objects.NewList(fields), nil
}

// readerGetattr exposes line_num on the reader.
//
// CPython: Modules/_csv.c:844 Reader_getattro
func readerGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	r, ok := o.(*Reader)
	if !ok {
		return objects.GenericGetAttr(o, name)
	}
	n, ok2 := name.(*objects.Unicode)
	if !ok2 {
		return nil, fmt.Errorf("TypeError: attribute name must be str")
	}
	if n.Value() == "line_num" {
		return objects.NewInt(r.lineNum), nil
	}
	if n.Value() == "dialect" {
		return r.dialect, nil
	}
	return nil, fmt.Errorf("AttributeError: 'reader' object has no attribute %q", n.Value())
}

// ---------------------------------------------------------------------------
// Writer
// ---------------------------------------------------------------------------

// Writer is the Python csv.writer object. The per-row record buffer
// lives on the writer instance so writerow re-uses its allocation
// across calls, matching CPython's WriterObj.rec.
//
// CPython: Modules/_csv.c:958 WriterObj
type Writer struct {
	objects.Header

	dialect *Dialect
	output  objects.Object // writable object (write method)
	state   writerState
}

// writerType is the Python type for csv.writer objects.
var writerType = newWriterType()

func newWriterType() *objects.Type {
	t := objects.NewType("writer", []*objects.Type{objects.ObjectType()})
	t.Getattro = writerGetattr
	return t
}

// writerGetattr exposes writerow/writerows methods.
//
// CPython: Modules/_csv.c:1090 Writer_getattro
func writerGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	w, ok := o.(*Writer)
	if !ok {
		return objects.GenericGetAttr(o, name)
	}
	n, ok2 := name.(*objects.Unicode)
	if !ok2 {
		return nil, fmt.Errorf("TypeError: attribute name must be str")
	}
	switch n.Value() {
	case "writerow":
		return objects.NewBuiltinFunction("writerow", func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
			return writerWriteRow(w, args, kwargs)
		}), nil
	case "writerows":
		return objects.NewBuiltinFunction("writerows", func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
			return writerWriteRows(w, args, kwargs)
		}), nil
	case "dialect":
		return w.dialect, nil
	}
	return nil, fmt.Errorf("AttributeError: 'writer' object has no attribute %q", n.Value())
}

// writerWriteRow ports csv_writerow_lock_held: iterate the row, derive
// the per-field `quoted` flag from the dialect's quoting mode, coerce
// non-str non-None entries via str(), drive joinAppend for each, then
// flush with the line terminator and call output.write(line).
//
// CPython: Modules/_csv.c:1327 csv_writerow_lock_held
func writerWriteRow(w *Writer, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: writerow() takes exactly 1 argument (%d given)", len(args))
	}
	it, err := objects.Iter(args[0])
	if err != nil {
		return nil, csvErr(fmt.Sprintf("iterable expected, not %s", args[0].Type().Name))
	}
	itType := it.Type()
	if itType.IterNext == nil {
		return nil, csvErr(fmt.Sprintf("iterable expected, not %s", args[0].Type().Name))
	}
	d := w.dialect
	w.state.joinReset()
	nullField := false
	for {
		field, err := itType.IterNext(it)
		if err != nil {
			if errors.Is(err, objects.ErrStopIteration) {
				break
			}
			return nil, err
		}
		quoted := quotedFor(field, d)
		nullField = field == objects.None()
		var fieldStr string
		var isNull bool
		switch v := field.(type) {
		case *objects.Unicode:
			fieldStr = v.Value()
		default:
			if nullField {
				isNull = true
			} else {
				s, err := objects.Str(field)
				if err != nil {
					return nil, err
				}
				fieldStr = s
			}
		}
		if err := w.state.joinAppend(d, fieldStr, isNull, quoted); err != nil {
			return nil, err
		}
	}
	if w.state.numFields > 0 && w.state.recLen == 0 {
		if d.quoting == quoteNone ||
			(nullField && (d.quoting == quoteStrings || d.quoting == quoteNotNull)) {
			return nil, csvErr("single empty field record must be quoted")
		}
		w.state.numFields--
		if err := w.state.joinAppend(d, "", true, true); err != nil {
			return nil, err
		}
	}
	w.state.joinAppendLineterminator(d)
	return callWrite(w.output, string(w.state.rec[:w.state.recLen]))
}

// writerWriteRows writes multiple rows.
//
// CPython: Modules/_csv.c:1045 csv_writerows
func writerWriteRows(w *Writer, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: writerows() takes exactly 1 argument (%d given)", len(args))
	}
	it, err := objects.Iter(args[0])
	if err != nil {
		return nil, err
	}
	itType := it.Type()
	if itType.IterNext == nil {
		return nil, fmt.Errorf("TypeError: writerows() argument must be iterable")
	}
	for {
		row, err := itType.IterNext(it)
		if err != nil {
			if errors.Is(err, objects.ErrStopIteration) {
				break
			}
			return nil, err
		}
		if _, err := writerWriteRow(w, []objects.Object{row}, nil); err != nil {
			return nil, err
		}
	}
	return objects.None(), nil
}

// callWrite calls output.write(line).
func callWrite(output objects.Object, line string) (objects.Object, error) {
	writeAttr, err := objects.GetAttr(output, objects.NewStr("write"))
	if err != nil {
		return nil, fmt.Errorf("TypeError: csv writer output must have a write method")
	}
	return objects.Call(writeAttr, objects.NewTuple([]objects.Object{objects.NewStr(line)}), nil)
}

// ---------------------------------------------------------------------------
// Module-level functions
// ---------------------------------------------------------------------------

// buildModule constructs the _csv module dict.
//
// CPython: Modules/_csv.c:1423 _csv_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_csv")
	d := m.Dict()

	// Constants.
	// CPython: Modules/_csv.c:1447 quoting constants
	for _, c := range []struct {
		name string
		val  int
	}{
		{"QUOTE_MINIMAL", quoteMinimal},
		{"QUOTE_ALL", quoteAll},
		{"QUOTE_NONNUMERIC", quoteNonNumeric},
		{"QUOTE_NONE", quoteNone},
		{"QUOTE_STRINGS", quoteStrings},
		{"QUOTE_NOTNULL", quoteNotNull},
	} {
		if err := d.SetItem(objects.NewStr(c.name), objects.NewInt(int64(c.val))); err != nil {
			return nil, err
		}
	}

	// Dialect type.
	if err := d.SetItem(objects.NewStr("Dialect"), dialectType); err != nil {
		return nil, err
	}

	// Functions.
	fns := []struct {
		name string
		fn   func([]objects.Object, map[string]objects.Object) (objects.Object, error)
	}{
		{"reader", csvReader},
		{"writer", csvWriter},
		{"register_dialect", registerDialect},
		{"unregister_dialect", unregisterDialect},
		{"get_dialect", getDialect},
		{"list_dialects", listDialects},
		{"field_size_limit", fieldSizeLimitFn},
	}
	for _, f := range fns {
		if err := d.SetItem(objects.NewStr(f.name), objects.NewBuiltinFunction(f.name, f.fn)); err != nil {
			return nil, err
		}
	}

	// Error class: csv.Error is a subclass of Exception.
	// CPython: Modules/_csv.c:1820 csv_module_exec (PyExc_Exception base)
	csvError := objects.NewType("Error", []*objects.Type{pyerrors.PyExc_Exception})
	csvErrorType = csvError
	if err := d.SetItem(objects.NewStr("Error"), csvError); err != nil {
		return nil, err
	}

	return m, nil
}

// csvReader implements _csv.reader(csvfile, dialect='excel', **fmtparams).
//
// CPython: Modules/_csv.c:795 csv_reader
func csvReader(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: reader() requires at least 1 argument")
	}
	source := args[0]

	dialectName := "excel"
	if len(args) >= 2 {
		n, ok := args[1].(*objects.Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: reader() dialect must be str")
		}
		dialectName = n.Value()
	}

	d, err := lookupDialect(dialectName)
	if err != nil {
		return nil, err
	}
	// Clone and apply per-call format params.
	dc := cloneDialect(d)
	if err := applyFmtParams(dc, kwargs); err != nil {
		return nil, err
	}

	it, err := objects.Iter(source)
	if err != nil {
		return nil, fmt.Errorf("TypeError: reader() argument 1 must be iterable: %v", err)
	}

	rdr := &Reader{
		dialect: dc,
		source:  source,
		it:      it,
	}
	rdr.Init(readerType)
	return rdr, nil
}

// csvWriter implements _csv.writer(csvfile, dialect='excel', **fmtparams).
//
// CPython: Modules/_csv.c:958 csv_writer
func csvWriter(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: writer() requires at least 1 argument")
	}
	output := args[0]

	dialectName := "excel"
	if len(args) >= 2 {
		n, ok := args[1].(*objects.Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: writer() dialect must be str")
		}
		dialectName = n.Value()
	}

	d, err := lookupDialect(dialectName)
	if err != nil {
		return nil, err
	}
	dc := cloneDialect(d)
	if err := applyFmtParams(dc, kwargs); err != nil {
		return nil, err
	}

	wrt := &Writer{
		dialect: dc,
		output:  output,
	}
	wrt.Init(writerType)
	return wrt, nil
}

// cloneDialect copies a Dialect so per-call fmt params do not mutate
// the registry entry. We copy only the data fields and re-initialize
// the Header so we do not copy the atomic refcount inside Header.
func cloneDialect(src *Dialect) *Dialect {
	d := &Dialect{
		delimiter:        src.delimiter,
		doublequote:      src.doublequote,
		escapechar:       src.escapechar,
		hasEscapechar:    src.hasEscapechar,
		lineterminator:   src.lineterminator,
		quotechar:        src.quotechar,
		hasQuotechar:     src.hasQuotechar,
		quoting:          src.quoting,
		skipinitialspace: src.skipinitialspace,
		strict:           src.strict,
	}
	d.Init(dialectType)
	return d
}

// registerDialect implements _csv.register_dialect(name, dialect=None, **fmtparams).
//
// CPython: Modules/_csv.c:1170 csv_register_dialect
func registerDialect(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: register_dialect() requires at least 1 argument")
	}
	nameObj, ok := args[0].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: register_dialect() name must be str")
	}
	name := nameObj.Value()

	base := newDialectDefault()
	if len(args) >= 2 {
		if src, ok2 := args[1].(*Dialect); ok2 {
			base = cloneDialect(src)
		}
	}
	if err := applyFmtParams(base, kwargs); err != nil {
		return nil, err
	}

	dialectMu.Lock()
	dialectRegistry[name] = base
	dialectMu.Unlock()
	return objects.None(), nil
}

// unregisterDialect implements _csv.unregister_dialect(name).
//
// CPython: Modules/_csv.c:1202 csv_unregister_dialect
func unregisterDialect(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: unregister_dialect() takes exactly 1 argument (%d given)", len(args))
	}
	nameObj, ok := args[0].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: unregister_dialect() name must be str")
	}
	name := nameObj.Value()
	dialectMu.Lock()
	_, found := dialectRegistry[name]
	if found {
		delete(dialectRegistry, name)
	}
	dialectMu.Unlock()
	if !found {
		return nil, csvErr(fmt.Sprintf("unknown dialect %q", name))
	}
	return objects.None(), nil
}

// getDialect implements _csv.get_dialect(name).
//
// CPython: Modules/_csv.c:1219 csv_get_dialect
func getDialect(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: get_dialect() takes exactly 1 argument (%d given)", len(args))
	}
	nameObj, ok := args[0].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: get_dialect() name must be str")
	}
	d, err := lookupDialect(nameObj.Value())
	if err != nil {
		return nil, err
	}
	return d, nil
}

// listDialects implements _csv.list_dialects().
//
// CPython: Modules/_csv.c:1232 csv_list_dialects
func listDialects(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	dialectMu.RLock()
	names := make([]objects.Object, 0, len(dialectRegistry))
	for name := range dialectRegistry {
		names = append(names, objects.NewStr(name))
	}
	dialectMu.RUnlock()
	return objects.NewList(names), nil
}

// fieldSizeLimitFn implements _csv.field_size_limit([limit]).
//
// CPython: Modules/_csv.c:1247 csv_field_size_limit
func fieldSizeLimitFn(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	old := fieldSizeLimit
	if len(args) == 1 {
		iv, ok := args[0].(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: field_size_limit() argument must be int")
		}
		n, _ := iv.Int64()
		fieldSizeLimit = int(n)
	} else if len(args) > 1 {
		return nil, fmt.Errorf("TypeError: field_size_limit() takes at most 1 argument (%d given)", len(args))
	}
	return objects.NewInt(int64(old)), nil
}
