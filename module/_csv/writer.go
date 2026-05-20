// CPython-faithful CSV writer state machine. Replaces the
// encoding/csv-backed `formatCSVRow` shim with a direct port of
// `Modules/_csv.c::join_append_data` / `join_append` /
// `join_append_lineterminator` so writer output honours the same
// dialect rules (doublequote, escapechar, quoting variants, QUOTE_ALL /
// QUOTE_STRINGS / QUOTE_NOTNULL, line terminator characters, and the
// single-empty-field rescue path).
//
// CPython: Modules/_csv.c:1147 join_append_data
// CPython: Modules/_csv.c:1260 join_append
// CPython: Modules/_csv.c:1303 join_append_lineterminator
// CPython: Modules/_csv.c:1327 csv_writerow_lock_held

package _csv

import (
	"fmt"
	"strings"

	"github.com/tamnd/gopy/objects"
)

// writerState holds the running record buffer for a single
// `csv.writer.writerow` call. The buffer is reset per row, matching
// CPython's `join_reset`.
//
// CPython: Modules/_csv.c:958 WriterObj
type writerState struct {
	rec       []rune
	recLen    int
	numFields int
}

// joinReset clears the record buffer between rows.
//
// CPython: Modules/_csv.c:1135 join_reset
func (w *writerState) joinReset() {
	w.recLen = 0
	w.numFields = 0
}

// joinAppendData is the two-pass field encoder. On `copyPhase=false`
// it walks the field counting characters and updating `*quoted`
// (mutating it to true if a special char demands wrapping). On
// `copyPhase=true` it writes the encoded bytes into `w.rec` starting
// at `w.recLen`.
//
// CPython: Modules/_csv.c:1147 join_append_data
func (w *writerState) joinAppendData(d *Dialect, field string, isNull bool, quoted *bool, copyPhase bool) (int, error) {
	recLen := w.recLen
	addch := func(c rune) {
		if copyPhase {
			w.rec[recLen] = c
		}
		recLen++
	}

	if w.numFields > 0 {
		addch(d.delimiter)
	}
	if copyPhase && *quoted {
		addch(d.quotechar)
	}

	if !isNull {
		for _, c := range field {
			wantEscape := false
			special := c == d.delimiter ||
				(d.hasEscapechar && c == d.escapechar) ||
				(d.hasQuotechar && c == d.quotechar) ||
				c == '\n' || c == '\r' ||
				strings.ContainsRune(d.lineterminator, c)
			if special {
				if d.quoting == quoteNone {
					wantEscape = true
				} else {
					if d.hasQuotechar && c == d.quotechar {
						if d.doublequote {
							addch(d.quotechar)
						} else {
							wantEscape = true
						}
					} else if d.hasEscapechar && c == d.escapechar {
						wantEscape = true
					}
					if !wantEscape {
						*quoted = true
					}
				}
				if wantEscape {
					if !d.hasEscapechar {
						return -1, fmt.Errorf("csv.Error: need to escape, but no escapechar set")
					}
					addch(d.escapechar)
				}
			}
			addch(c)
		}
	}

	if *quoted {
		if copyPhase {
			addch(d.quotechar)
		} else {
			recLen += 2
		}
	}
	return recLen, nil
}

// joinCheckRecSize grows `rec` so writes up through `recLen-1` are in
// bounds. CPython uses MEM_INCR-block resizing; the Go slice grows on
// append, so we just push zero runes to extend.
//
// CPython: Modules/_csv.c:1241 join_check_rec_size
func (w *writerState) joinCheckRecSize(recLen int) {
	for len(w.rec) < recLen {
		w.rec = append(w.rec, 0)
	}
}

// joinAppend runs the two-pass encode of a single field.
//
// CPython: Modules/_csv.c:1260 join_append
func (w *writerState) joinAppend(d *Dialect, field string, isNull bool, quoted bool) error {
	fieldLen := len([]rune(field))
	if isNull {
		fieldLen = 0
	}
	if fieldLen == 0 && d.delimiter == ' ' && d.skipinitialspace {
		if d.quoting == quoteNone ||
			(isNull && (d.quoting == quoteStrings || d.quoting == quoteNotNull)) {
			return fmt.Errorf("csv.Error: empty field must be quoted if delimiter is a space and skipinitialspace is true")
		}
		quoted = true
	}
	recLen, err := w.joinAppendData(d, field, isNull, &quoted, false)
	if err != nil {
		return err
	}
	w.joinCheckRecSize(recLen)
	newLen, err := w.joinAppendData(d, field, isNull, &quoted, true)
	if err != nil {
		return err
	}
	w.recLen = newLen
	w.numFields++
	return nil
}

// joinAppendLineterminator appends the dialect lineterminator to the
// record buffer.
//
// CPython: Modules/_csv.c:1303 join_append_lineterminator
func (w *writerState) joinAppendLineterminator(d *Dialect) {
	termRunes := []rune(d.lineterminator)
	w.joinCheckRecSize(w.recLen + len(termRunes))
	for _, c := range termRunes {
		w.rec[w.recLen] = c
		w.recLen++
	}
}

// quotedFor mirrors the `switch (dialect->quoting)` block in
// csv_writerow_lock_held that derives the initial `quoted` flag.
//
// CPython: Modules/_csv.c:1351 csv_writerow_lock_held
func quotedFor(field objects.Object, d *Dialect) bool {
	switch d.quoting {
	case quoteNonNumeric:
		return !pyNumberCheck(field)
	case quoteAll:
		return true
	case quoteStrings:
		_, ok := field.(*objects.Unicode)
		return ok
	case quoteNotNull:
		return field != objects.None()
	default:
		return false
	}
}

// pyNumberCheck mirrors `PyNumber_Check`: int / float / bool / complex
// all count as numbers. We have no complex type yet, so int/float/bool
// covers everything CPython would accept here.
//
// CPython: Objects/abstract.c PyNumber_Check
func pyNumberCheck(o objects.Object) bool {
	switch o.(type) {
	case *objects.Int, *objects.Float, *objects.Bool:
		return true
	}
	return false
}
