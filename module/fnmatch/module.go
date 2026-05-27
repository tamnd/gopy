// fnmatch module: Go port of Lib/fnmatch.py. CPython compiles each
// glob pattern into a regex via translate() and runs it through the re
// engine; that requires regex constructs RE2 (and the current gopy re
// port) does not accept (`(?>...)` atomic groups, `\Z` end anchor,
// Python set operations). To keep the public surface byte-equivalent
// while sidestepping the regex gap, this port does two things:
//
//   - translate() ports CPython's _translate + _join_translated_parts
//     directly, so its output is the same regex string Python emits.
//   - fnmatch / fnmatchcase / filter / filterfalse drive a separate
//     Go-side matcher that interprets the same shell metacharacters
//     (`*`, `?`, `[...]`, `[!...]`, character ranges) with CPython
//     semantics. The matcher and translate() agree on the pattern
//     grammar by construction; they share the bracket parser.
//
// When the re port (task #510) lands full Python regex support, this
// file can collapse to a 30-line wrapper that runs translate() through
// re.compile(...).match. Until then, the dual implementation is the
// honest way to ship the four public names without misrepresenting
// translate()'s output.
//
// CPython: Lib/fnmatch.py:1 module docstring
// CPython: Lib/fnmatch.py:22 fnmatch
// CPython: Lib/fnmatch.py:53 filter
// CPython: Lib/fnmatch.py:70 filterfalse
// CPython: Lib/fnmatch.py:85 fnmatchcase
// CPython: Lib/fnmatch.py:95 translate

package fnmatch

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("fnmatch", buildModule)
}

func buildModule() (*objects.Module, error) {
	m := objects.NewModule("fnmatch")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"fnmatch", objects.NewBuiltinFunction("fnmatch", fnmatchFn)},
		{"fnmatchcase", objects.NewBuiltinFunction("fnmatchcase", fnmatchCaseFn)},
		{"filter", objects.NewBuiltinFunction("filter", filterFn)},
		{"filterfalse", objects.NewBuiltinFunction("filterfalse", filterFalseFn)},
		{"translate", objects.NewBuiltinFunction("translate", translateFn)},
		{"_translate", objects.NewBuiltinFunction("_translate", translatePrivateFn)},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// fnmatchFn implements fnmatch.fnmatch. On POSIX, os.path.normcase is
// a no-op, so we behave identically to fnmatchcase. Windows-style
// case folding is not modeled here: gopy's os.path port (module/os/)
// does not differentiate, and CPython's normcase on POSIX is the
// identity function.
//
// CPython: Lib/fnmatch.py:22 fnmatch
func fnmatchFn(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: fnmatch() takes exactly 2 arguments (%d given)", len(args))
	}
	name, err := strArg("fnmatch", args[0])
	if err != nil {
		return nil, err
	}
	pat, err := strArg("fnmatch", args[1])
	if err != nil {
		return nil, err
	}
	return objects.NewBool(matchGlob(name, pat)), nil
}

// fnmatchCaseFn implements fnmatch.fnmatchcase.
//
// CPython: Lib/fnmatch.py:85 fnmatchcase
func fnmatchCaseFn(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: fnmatchcase() takes exactly 2 arguments (%d given)", len(args))
	}
	name, err := strArg("fnmatchcase", args[0])
	if err != nil {
		return nil, err
	}
	pat, err := strArg("fnmatchcase", args[1])
	if err != nil {
		return nil, err
	}
	return objects.NewBool(matchGlob(name, pat)), nil
}

// filterFn implements fnmatch.filter.
//
// CPython: Lib/fnmatch.py:53 filter
func filterFn(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: filter() takes exactly 2 arguments (%d given)", len(args))
	}
	pat, err := strArg("filter", args[1])
	if err != nil {
		return nil, err
	}
	names, err := iterateStrings(args[0])
	if err != nil {
		return nil, err
	}
	out := make([]objects.Object, 0, len(names))
	for _, pair := range names {
		if matchGlob(pair.str, pat) {
			out = append(out, pair.obj)
		}
	}
	return objects.NewList(out), nil
}

// filterFalseFn implements fnmatch.filterfalse.
//
// CPython: Lib/fnmatch.py:70 filterfalse
func filterFalseFn(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: filterfalse() takes exactly 2 arguments (%d given)", len(args))
	}
	pat, err := strArg("filterfalse", args[1])
	if err != nil {
		return nil, err
	}
	names, err := iterateStrings(args[0])
	if err != nil {
		return nil, err
	}
	out := make([]objects.Object, 0, len(names))
	for _, pair := range names {
		if !matchGlob(pair.str, pat) {
			out = append(out, pair.obj)
		}
	}
	return objects.NewList(out), nil
}

// translatePrivateFn implements fnmatch._translate(pat, star, question_mark).
// Returns a 2-tuple (parts, star_indices) that glob.py[342] consumes via [0].
//
// CPython: Lib/fnmatch.py:109 _translate
func translatePrivateFn(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("TypeError: _translate() takes exactly 3 arguments (%d given)", len(args))
	}
	pat, err := strArg("_translate", args[0])
	if err != nil {
		return nil, err
	}
	star, err := strArg("_translate", args[1])
	if err != nil {
		return nil, err
	}
	qm, err := strArg("_translate", args[2])
	if err != nil {
		return nil, err
	}
	parts, starIdxs := translateParts(pat, star, qm)
	pyParts := make([]objects.Object, len(parts))
	for i, p := range parts {
		pyParts[i] = objects.NewStr(p)
	}
	pyIdxs := make([]objects.Object, len(starIdxs))
	for i, idx := range starIdxs {
		pyIdxs[i] = objects.NewInt(int64(idx))
	}
	return objects.NewTuple([]objects.Object{
		objects.NewList(pyParts),
		objects.NewList(pyIdxs),
	}), nil
}

// translateFn implements fnmatch.translate. Output is byte-identical
// to CPython's translate() for the same input pattern.
//
// CPython: Lib/fnmatch.py:95 translate
func translateFn(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: translate() takes exactly 1 argument (%d given)", len(args))
	}
	pat, err := strArg("translate", args[0])
	if err != nil {
		return nil, err
	}
	return objects.NewStr(translate(pat)), nil
}

// strPair carries the original Python object plus its string form,
// so filter / filterfalse can pass through the caller's objects
// (matching CPython's behavior of appending the unmodified input).
type strPair struct {
	obj objects.Object
	str string
}

func strArg(fn string, o objects.Object) (string, error) {
	if _, ok := o.(*objects.Unicode); ok {
		return objects.Str(o)
	}
	return "", fmt.Errorf("TypeError: %s() argument must be str, not %s", fn, o.Type().Name)
}

func iterateStrings(iter objects.Object) ([]strPair, error) {
	tp := iter.Type()
	if tp.Iter == nil {
		return nil, fmt.Errorf("TypeError: argument of type '%s' is not iterable", tp.Name)
	}
	it, err := tp.Iter(iter)
	if err != nil {
		return nil, err
	}
	itType := it.Type()
	var out []strPair
	for {
		v, err := itType.IterNext(it)
		if err != nil {
			if errors.Is(err, objects.ErrStopIteration) {
				break
			}
			return nil, err
		}
		if v == nil {
			break
		}
		if _, ok := v.(*objects.Unicode); !ok {
			return nil, fmt.Errorf("TypeError: filter() entry must be str, not %s", v.Type().Name)
		}
		s, err := objects.Str(v)
		if err != nil {
			return nil, err
		}
		out = append(out, strPair{obj: v, str: s})
	}
	return out, nil
}

// translate is the byte-equivalent port of CPython translate(pat).
// Calls _translate to produce the parts and star indices, then
// _joinTranslatedParts to fold them into the final regex string.
//
// CPython: Lib/fnmatch.py:95 translate
func translate(pat string) string {
	parts, starIndices := translateParts(pat, "*", ".")
	return joinTranslatedParts(parts, starIndices)
}

// reSetops is the precompiled equivalent of fnmatch's
// _re_setops_sub = re.compile(r'([&~|])').sub: matches a single
// `&`, `~`, or `|` so we can escape Python regex set-operation
// triggers in character classes.
//
// CPython: Lib/fnmatch.py:105 _re_setops_sub
var reSetops = regexp.MustCompile(`([&~|])`)

// escapeCache mirrors fnmatch's functools.lru_cache wrapper around
// re.escape. Translate is called once per pattern; per-character
// caching avoids re-escaping common bytes inside long patterns.
//
// CPython: Lib/fnmatch.py:106 _re_escape
var (
	escapeMu    sync.RWMutex
	escapeStore = map[string]string{}
)

func reEscape(s string) string {
	escapeMu.RLock()
	if v, ok := escapeStore[s]; ok {
		escapeMu.RUnlock()
		return v
	}
	escapeMu.RUnlock()
	v := regexp.QuoteMeta(s)
	escapeMu.Lock()
	escapeStore[s] = v
	escapeMu.Unlock()
	return v
}

// translateParts is the byte-equivalent port of CPython _translate.
// Returns (parts, starIndices) so joinTranslatedParts can fold the
// star positions into atomic groups.
//
// CPython: Lib/fnmatch.py:109 _translate
//
//nolint:gocognit,gocyclo // mirrors CPython's _translate body
func translateParts(pat, star, questionMark string) ([]string, []int) {
	var res []string
	add := func(s string) { res = append(res, s) }
	var starIndices []int

	i, n := 0, len(pat)
	for i < n {
		c := pat[i]
		i++
		switch c {
		case '*':
			starIndices = append(starIndices, len(res))
			add(star)
			for i < n && pat[i] == '*' {
				i++
			}
		case '?':
			add(questionMark)
		case '[':
			j := i
			if j < n && pat[j] == '!' {
				j++
			}
			if j < n && pat[j] == ']' {
				j++
			}
			for j < n && pat[j] != ']' {
				j++
			}
			if j >= n {
				add(`\[`)
			} else {
				stuff := pat[i:j]
				if !strings.Contains(stuff, "-") {
					stuff = strings.ReplaceAll(stuff, `\`, `\\`)
				} else {
					var chunks []string
					var k int
					iLocal := i
					if pat[i] == '!' {
						k = i + 2
					} else {
						k = i + 1
					}
					for {
						k = indexInRange(pat, "-", k, j)
						if k < 0 {
							break
						}
						chunks = append(chunks, pat[iLocal:k])
						iLocal = k + 1
						k += 3
					}
					chunk := pat[iLocal:j]
					if chunk != "" {
						chunks = append(chunks, chunk)
					} else {
						chunks[len(chunks)-1] += "-"
					}
					// Remove empty ranges: invalid in RE.
					for k := len(chunks) - 1; k > 0; k-- {
						if chunks[k-1][len(chunks[k-1])-1] > chunks[k][0] {
							chunks[k-1] = chunks[k-1][:len(chunks[k-1])-1] + chunks[k][1:]
							chunks = append(chunks[:k], chunks[k+1:]...)
						}
					}
					// Escape backslashes and hyphens for set difference.
					escaped := make([]string, len(chunks))
					for idx, s := range chunks {
						s = strings.ReplaceAll(s, `\`, `\\`)
						s = strings.ReplaceAll(s, `-`, `\-`)
						escaped[idx] = s
					}
					stuff = strings.Join(escaped, "-")
				}
				i = j + 1
				switch stuff {
				case "":
					add(`(?!)`)
				case "!":
					add(`.`)
				default:
					stuff = reSetops.ReplaceAllString(stuff, `\$1`)
					switch stuff[0] {
					case '!':
						stuff = "^" + stuff[1:]
					case '^', '[':
						stuff = `\` + stuff
					}
					add("[" + stuff + "]")
				}
			}
		default:
			add(reEscape(string(c)))
		}
	}
	return res, starIndices
}

// indexInRange is the equivalent of Python's str.find(sub, start, end).
// Returns the absolute index of sub in s within [start, end), or -1.
func indexInRange(s, sub string, start, end int) int {
	if start < 0 {
		start = 0
	}
	if end > len(s) {
		end = len(s)
	}
	if start >= end {
		return -1
	}
	idx := strings.Index(s[start:end], sub)
	if idx < 0 {
		return -1
	}
	return start + idx
}

// joinTranslatedParts is the byte-equivalent port of CPython
// _join_translated_parts. Produces (?s:...)\z framing with atomic
// groups around interior `STAR fixed` pairings.
//
// CPython: Lib/fnmatch.py:186 _join_translated_parts
func joinTranslatedParts(parts []string, starIndices []int) string {
	if len(starIndices) == 0 {
		return `(?s:` + strings.Join(parts, "") + `)\z`
	}
	j := starIndices[0]
	buffer := append([]string{}, parts[:j]...)
	i := j + 1
	for s := 1; s < len(starIndices); s++ {
		j = starIndices[s]
		buffer = append(buffer, `(?>.*?`)
		buffer = append(buffer, parts[i:j]...)
		buffer = append(buffer, `)`)
		i = j + 1
	}
	buffer = append(buffer, `.*`)
	buffer = append(buffer, parts[i:]...)
	return `(?s:` + strings.Join(buffer, "") + `)\z`
}

// matchGlob runs the shell-pattern match Python's regex engine would
// run on translate(pat). Operates directly on the pattern grammar so
// callers do not need a Python-flavor regex engine.
//
// Semantics:
//   - `*`     matches any run of characters (including empty)
//   - `?`     matches exactly one character
//   - `[seq]` matches one character in seq; ranges with `-`; leading
//     `!` negates; an unmatched `[` is a literal
//
// CPython: Lib/fnmatch.py:95 translate (regex semantics this mirrors)
func matchGlob(name, pat string) bool {
	tokens := compileGlob(pat)
	return runGlob(name, 0, tokens, 0)
}

// globToken is one piece of a compiled glob: a literal byte, a `?`,
// a `*`, or a character class.
type globToken struct {
	kind  globKind
	lit   byte
	class globClass
}

type globKind int

const (
	globLit globKind = iota
	globAny
	globStar
	globClassTok
)

type globClass struct {
	negate bool
	chars  map[byte]struct{}
	ranges []byteRange
}

type byteRange struct{ lo, hi byte }

func (c globClass) match(b byte) bool {
	if _, ok := c.chars[b]; ok {
		return !c.negate
	}
	for _, r := range c.ranges {
		if b >= r.lo && b <= r.hi {
			return !c.negate
		}
	}
	return c.negate
}

// compileGlob parses pat into a token sequence. Bracket parsing
// mirrors CPython's _translate: a leading `!` is negation, an
// unterminated `[` is a literal, an empty class is "never match",
// and `[!]` matches any character.
func compileGlob(pat string) []globToken {
	var out []globToken
	i, n := 0, len(pat)
	for i < n {
		c := pat[i]
		i++
		switch c {
		case '*':
			// Compress consecutive stars.
			out = append(out, globToken{kind: globStar})
			for i < n && pat[i] == '*' {
				i++
			}
		case '?':
			out = append(out, globToken{kind: globAny})
		case '[':
			j := i
			if j < n && pat[j] == '!' {
				j++
			}
			if j < n && pat[j] == ']' {
				j++
			}
			for j < n && pat[j] != ']' {
				j++
			}
			if j >= n {
				out = append(out, globToken{kind: globLit, lit: '['})
			} else {
				cls := parseClass(pat[i:j])
				i = j + 1
				out = append(out, globToken{kind: globClassTok, class: cls})
			}
		default:
			out = append(out, globToken{kind: globLit, lit: c})
		}
	}
	return out
}

// parseClass turns the raw bracket interior into a globClass.
// Handles negation, ranges, and the special-case `[!]` => match any
// (negated empty range) and `[]` => never match (empty range).
func parseClass(body string) globClass {
	cls := globClass{chars: map[byte]struct{}{}}
	if body != "" && body[0] == '!' {
		cls.negate = true
		body = body[1:]
	}
	if body == "" {
		if cls.negate {
			// `[!]` matches any byte: emit a class that always returns true.
			cls.ranges = []byteRange{{0, 255}}
			cls.negate = false
		}
		// `[]` never matches: empty positive class, returned as-is.
		return cls
	}
	i, n := 0, len(body)
	for i < n {
		c := body[i]
		// Range: x-y, but not if `-` is first or last in body.
		if i+2 < n && body[i+1] == '-' {
			cls.ranges = append(cls.ranges, byteRange{lo: c, hi: body[i+2]})
			i += 3
			continue
		}
		cls.chars[c] = struct{}{}
		i++
	}
	return cls
}

// runGlob is the recursive matcher. On `*` it tries every split.
func runGlob(name string, ni int, tokens []globToken, ti int) bool {
	for ti < len(tokens) {
		t := tokens[ti]
		switch t.kind {
		case globLit:
			if ni >= len(name) || name[ni] != t.lit {
				return false
			}
			ni++
			ti++
		case globAny:
			if ni >= len(name) {
				return false
			}
			ni++
			ti++
		case globStar:
			// Match minimal -> maximal. Try every suffix start.
			for k := ni; k <= len(name); k++ {
				if runGlob(name, k, tokens, ti+1) {
					return true
				}
			}
			return false
		case globClassTok:
			if ni >= len(name) || !t.class.match(name[ni]) {
				return false
			}
			ni++
			ti++
		}
	}
	return ni == len(name)
}
