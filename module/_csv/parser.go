// CPython-faithful CSV parser state machine. Replaces the
// encoding/csv-backed `parseCSVLine` shim with a direct port of
// `Modules/_csv.c::parse_process_char` so the reader honours the same
// dialect rules (strict mode, doublequote, escapechar, quoting variants
// QUOTE_NONNUMERIC / QUOTE_NOTNULL / QUOTE_STRINGS, line continuation
// inside quoted fields, EAT_CRNL, ESCAPED_CHAR EOL handling) byte for
// byte.
//
// CPython: Modules/_csv.c:706 parse_process_char

package _csv

import (
	"fmt"
	"strconv"

	"github.com/tamnd/gopy/objects"
)

// Parser state constants. Mirrored from the unnamed enum in
// `Modules/_csv.c:80`.
//
// CPython: Modules/_csv.c:80 ParserState
const (
	psStartRecord = iota
	psStartField
	psEscapedChar
	psInField
	psInQuotedField
	psEscapeInQuotedField
	psQuoteInQuotedField
	psEatCrnl
	psAfterEscapedCrnl
)

// eol is the sentinel value parse_process_char uses to mark end of
// line. CPython uses `((Py_UCS4)-1)`; we use -1 in a int32 since Go's
// rune is signed and zero is a valid character.
//
// CPython: Modules/_csv.c:108 EOL
const eol = rune(-1)

// readerState holds the in-flight parser state for a single
// `csv.reader` instance. Two `Reader` instances therefore parse
// independently, matching CPython's per-ReaderObj field layout.
//
// CPython: Modules/_csv.c:795 ReaderObj
type readerState struct {
	state         int
	field         []rune
	fields        []objects.Object
	unquotedField bool
}

// resetParser empties the field buffer and the running record. Called
// once per `Reader.__next__`.
//
// CPython: Modules/_csv.c:913 parse_reset
func (s *readerState) resetParser() {
	s.state = psStartRecord
	s.field = s.field[:0]
	s.fields = s.fields[:0]
	s.unquotedField = false
}

// addChar appends one character to the in-flight field buffer.
//
// CPython: Modules/_csv.c:714 parse_add_char
func (s *readerState) addChar(c rune) error {
	if len(s.field) >= fieldSizeLimit {
		return csvErr(fmt.Sprintf("field larger than field limit (%d)", fieldSizeLimit))
	}
	s.field = append(s.field, c)
	return nil
}

// saveField commits the running field buffer as the next record entry.
// Honours QUOTE_NONNUMERIC / QUOTE_STRINGS (unquoted -> float) and
// QUOTE_NOTNULL / QUOTE_STRINGS (empty unquoted -> None).
//
// CPython: Modules/_csv.c:658 parse_save_field
func (s *readerState) saveField(d *Dialect) error {
	q := d.quoting
	if s.unquotedField && len(s.field) == 0 &&
		(q == quoteNotNull || q == quoteStrings) {
		s.fields = append(s.fields, objects.None())
		return nil
	}
	str := string(s.field)
	s.field = s.field[:0]
	if s.unquotedField && len(str) > 0 &&
		(q == quoteNonNumeric || q == quoteStrings) {
		f, err := strconv.ParseFloat(str, 64)
		if err != nil {
			return csvErr(fmt.Sprintf("bad value for QUOTE_NONNUMERIC field %q", str))
		}
		s.fields = append(s.fields, objects.NewFloat(f))
		return nil
	}
	s.fields = append(s.fields, objects.NewStr(str))
	return nil
}

// processChar drives one character through the state machine. The
// state transitions follow `parse_process_char` line for line so the
// reader's output matches CPython on every dialect knob.
//
// CPython: Modules/_csv.c:730 parse_process_char
func (s *readerState) processChar(d *Dialect, c rune) error {
	switch s.state {
	case psStartRecord:
		if c == eol {
			return nil
		}
		if c == '\n' || c == '\r' {
			s.state = psEatCrnl
			return nil
		}
		s.state = psStartField
		fallthrough

	case psStartField:
		s.unquotedField = true
		switch {
		case c == '\n' || c == '\r' || c == eol:
			if err := s.saveField(d); err != nil {
				return err
			}
			if c == eol {
				s.state = psStartRecord
			} else {
				s.state = psEatCrnl
			}
		case d.hasQuotechar && c == d.quotechar && d.quoting != quoteNone:
			s.unquotedField = false
			s.state = psInQuotedField
		case d.hasEscapechar && c == d.escapechar:
			s.state = psEscapedChar
		case c == ' ' && d.skipinitialspace:
			// drop initial space
		case c == d.delimiter:
			if err := s.saveField(d); err != nil {
				return err
			}
		default:
			if err := s.addChar(c); err != nil {
				return err
			}
			s.state = psInField
		}

	case psEscapedChar:
		if c == '\n' || c == '\r' {
			if err := s.addChar(c); err != nil {
				return err
			}
			s.state = psAfterEscapedCrnl
			return nil
		}
		if c == eol {
			c = '\n'
		}
		if err := s.addChar(c); err != nil {
			return err
		}
		s.state = psInField

	case psAfterEscapedCrnl:
		if c == eol {
			return nil
		}
		// fallthrough to IN_FIELD
		s.state = psInField
		return s.processChar(d, c)

	case psInField:
		switch {
		case c == '\n' || c == '\r' || c == eol:
			if err := s.saveField(d); err != nil {
				return err
			}
			if c == eol {
				s.state = psStartRecord
			} else {
				s.state = psEatCrnl
			}
		case d.hasEscapechar && c == d.escapechar:
			s.state = psEscapedChar
		case c == d.delimiter:
			if err := s.saveField(d); err != nil {
				return err
			}
			s.state = psStartField
		default:
			if err := s.addChar(c); err != nil {
				return err
			}
		}

	case psInQuotedField:
		switch {
		case c == eol:
			// keep reading; quoted field can span lines
		case d.hasEscapechar && c == d.escapechar:
			s.state = psEscapeInQuotedField
		case d.hasQuotechar && c == d.quotechar && d.quoting != quoteNone:
			if d.doublequote {
				s.state = psQuoteInQuotedField
			} else {
				s.state = psInField
			}
		default:
			if err := s.addChar(c); err != nil {
				return err
			}
		}

	case psEscapeInQuotedField:
		if c == eol {
			c = '\n'
		}
		if err := s.addChar(c); err != nil {
			return err
		}
		s.state = psInQuotedField

	case psQuoteInQuotedField:
		switch {
		case d.quoting != quoteNone && d.hasQuotechar && c == d.quotechar:
			if err := s.addChar(c); err != nil {
				return err
			}
			s.state = psInQuotedField
		case c == d.delimiter:
			if err := s.saveField(d); err != nil {
				return err
			}
			s.state = psStartField
		case c == '\n' || c == '\r' || c == eol:
			if err := s.saveField(d); err != nil {
				return err
			}
			if c == eol {
				s.state = psStartRecord
			} else {
				s.state = psEatCrnl
			}
		case !d.strict:
			if err := s.addChar(c); err != nil {
				return err
			}
			s.state = psInField
		default:
			return fmt.Errorf("csv.Error: %q expected after %q", d.delimiter, d.quotechar)
		}

	case psEatCrnl:
		switch {
		case c == '\n' || c == '\r':
			// stay in EAT_CRNL
		case c == eol:
			s.state = psStartRecord
		default:
			return fmt.Errorf("csv.Error: new-line character seen in unquoted field - do you need to open the file with newline=''?")
		}
	}
	return nil
}
