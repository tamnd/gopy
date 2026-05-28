package _sre

// Match-object methods. The Match instance carries the engine's mark
// array and the matched string; CPython lazily reads start / end /
// substring through pattern_subx and pattern_lookup_group helpers.
// This file ports that surface: group / groups / groupdict / start /
// end / span / expand / regs / lastgroup, with named-group lookup
// resolved through the Pattern's `groupindex` dict.
//
// CPython: Modules/_sre/sre.c:2900 pattern_new_match
// CPython: Modules/_sre/sre.c:2980 match methods block

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tamnd/gopy/objects"
)

// getMatchLocs reads the matchData side store. Returns (locs, src).
//
// CPython: Modules/_sre/sre.c:2900 pattern_new_match (Match storage)
func getMatchLocs(inst *objects.Instance) ([]int, string, error) {
	md, ok := matchStore[inst]
	if !ok {
		return nil, "", fmt.Errorf("Match has no locs data")
	}
	return md.locs, md.s, nil
}

// getMatchData returns the engine payload alongside the locs/string
// pair so callers can branch on isBytes when materialising substrings.
func getMatchData(inst *objects.Instance) (*matchData, error) {
	md, ok := matchStore[inst]
	if !ok {
		return nil, fmt.Errorf("Match has no locs data")
	}
	return md, nil
}

// matchSlice mirrors getslice / PATTERN_TYPE_BYTES branching in
// Modules/_sre/sre.c:2735 match_getslice_by_index: a bytes-input
// match rebuilds substrings as Bytes; a str-input match keeps
// Unicode.
//
// The engine runs over []int32 code points (state.input), so locs[] are
// code-point indices. md.s is the original Go string in UTF-8 bytes for
// the str path, or raw bytes for the bytes path. Slicing md.s[lo:hi]
// directly is correct for bytes mode (1 byte per code point) but wrong
// for the str path when the input carries any non-ASCII character.
// Convert through []rune so the substring corresponds to code points,
// matching CPython's PyUnicode_Substring.
func matchSlice(md *matchData, lo, hi int) objects.Object {
	if md.isBytes {
		return objects.NewBytes([]byte(md.s[lo:hi]))
	}
	if md.runes == nil {
		md.runes = []rune(md.s)
	}
	if lo < 0 {
		lo = 0
	}
	if hi < 0 {
		hi = 0
	}
	if hi > len(md.runes) {
		hi = len(md.runes)
	}
	if lo > hi {
		lo = hi
	}
	return objects.NewStr(string(md.runes[lo:hi]))
}

// resolveGroup turns a group spec (int or str) into a 0-based group
// number. Group 0 is the whole match; named groups go through the
// Pattern's groupindex dict.
//
// CPython: Modules/_sre/sre.c:2870 match_getindex
func resolveGroup(inst *objects.Instance, arg objects.Object) (int, error) {
	if i, ok := arg.(*objects.Int); ok {
		v, _ := i.Int64()
		return int(v), nil
	}
	if s, ok := arg.(*objects.Unicode); ok {
		reObj, err := inst.Dict().GetItem(objects.NewStr("re"))
		if err != nil {
			return -1, fmt.Errorf("IndexError: no such group")
		}
		patInst, ok := reObj.(*objects.Instance)
		if !ok {
			return -1, fmt.Errorf("IndexError: no such group")
		}
		giObj, err := patInst.Dict().GetItem(objects.NewStr("groupindex"))
		if err != nil {
			return -1, fmt.Errorf("IndexError: no such group")
		}
		gi, ok := giObj.(*objects.Dict)
		if !ok {
			return -1, fmt.Errorf("IndexError: no such group")
		}
		v, err := gi.GetItem(objects.NewStr(s.Value()))
		if err != nil {
			return -1, fmt.Errorf("IndexError: no such group")
		}
		gid, ok := v.(*objects.Int)
		if !ok {
			return -1, fmt.Errorf("IndexError: no such group")
		}
		n, _ := gid.Int64()
		return int(n), nil
	}
	return -1, fmt.Errorf("IndexError: no such group")
}

// groupSpan returns (start, end) for group g, or (-1, -1) if the
// group did not match. Out-of-range group raises IndexError.
func groupSpan(locs []int, g int) (int, int, error) {
	if g < 0 || 2*g+1 >= len(locs) {
		return -1, -1, fmt.Errorf("IndexError: no such group")
	}
	return locs[2*g], locs[2*g+1], nil
}

// groupSubstring returns the matched substring for group g, or None
// if unmatched. Out-of-range raises IndexError.
func groupSubstring(md *matchData, g int) (objects.Object, error) {
	lo, hi, err := groupSpan(md.locs, g)
	if err != nil {
		return nil, err
	}
	if lo < 0 {
		return objects.None(), nil
	}
	return matchSlice(md, lo, hi), nil
}

// matchInst extracts the Instance and locs/string out of a method call
// frame. Used by every Match.* handler.
func matchInst(args []objects.Object, name string) (*objects.Instance, []int, string, error) {
	if len(args) < 1 {
		return nil, nil, "", fmt.Errorf("TypeError: %s() requires Match self", name)
	}
	inst, ok := args[0].(*objects.Instance)
	if !ok {
		return nil, nil, "", fmt.Errorf("TypeError: %s() requires Match", name)
	}
	locs, s, err := getMatchLocs(inst)
	return inst, locs, s, err
}

// matchGroup is the Phase 5 group() with int + str arg support.
//
// CPython: Modules/_sre/sre.c:2980 _sre_SRE_Match_group_impl
func matchGroup(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	inst, _, _, err := matchInst(args, "group")
	if err != nil {
		return nil, err
	}
	md, err := getMatchData(inst)
	if err != nil {
		return nil, err
	}
	if len(args) == 1 {
		return matchSlice(md, md.locs[0], md.locs[1]), nil
	}
	if len(args) == 2 {
		g, gerr := resolveGroup(inst, args[1])
		if gerr != nil {
			return nil, gerr
		}
		return groupSubstring(md, g)
	}
	out := make([]objects.Object, len(args)-1)
	for i, a := range args[1:] {
		g, gerr := resolveGroup(inst, a)
		if gerr != nil {
			return nil, gerr
		}
		sub, serr := groupSubstring(md, g)
		if serr != nil {
			return nil, serr
		}
		out[i] = sub
	}
	return objects.NewTuple(out), nil
}

// matchGroups returns the tuple of all groups 1..N. Each unmatched
// group is replaced with the `default` keyword argument (None if not
// passed).
//
// CPython: Modules/_sre/sre.c:3005 _sre_SRE_Match_groups_impl
func matchGroups(args []objects.Object, kw map[string]objects.Object) (objects.Object, error) {
	inst, _, _, err := matchInst(args, "groups")
	if err != nil {
		return nil, err
	}
	md, err := getMatchData(inst)
	if err != nil {
		return nil, err
	}
	def := objects.Object(objects.None())
	if kw != nil {
		if v, ok := kw["default"]; ok {
			def = v
		}
	}
	if len(args) >= 2 {
		def = args[1]
	}
	pairs := len(md.locs) / 2
	if pairs < 1 {
		return objects.NewTuple(nil), nil
	}
	out := make([]objects.Object, pairs-1)
	for g := 1; g < pairs; g++ {
		lo, hi := md.locs[2*g], md.locs[2*g+1]
		if lo < 0 || hi < 0 {
			out[g-1] = def
		} else {
			out[g-1] = matchSlice(md, lo, hi)
		}
	}
	return objects.NewTuple(out), nil
}

// matchGroupdict returns a dict mapping every named group to its
// matched substring (or default if unmatched).
//
// CPython: Modules/_sre/sre.c:3023 _sre_SRE_Match_groupdict_impl
func matchGroupdict(args []objects.Object, kw map[string]objects.Object) (objects.Object, error) {
	inst, _, _, err := matchInst(args, "groupdict")
	if err != nil {
		return nil, err
	}
	md, err := getMatchData(inst)
	if err != nil {
		return nil, err
	}
	def := objects.Object(objects.None())
	if kw != nil {
		if v, ok := kw["default"]; ok {
			def = v
		}
	}
	if len(args) >= 2 {
		def = args[1]
	}
	reObj, gerr := inst.Dict().GetItem(objects.NewStr("re"))
	if gerr != nil {
		return objects.NewDict(), nil
	}
	patInst, ok := reObj.(*objects.Instance)
	if !ok {
		return objects.NewDict(), nil
	}
	giObj, gerr := patInst.Dict().GetItem(objects.NewStr("groupindex"))
	if gerr != nil {
		return objects.NewDict(), nil
	}
	gi, ok := giObj.(*objects.Dict)
	if !ok {
		return objects.NewDict(), nil
	}
	out := objects.NewDict()
	for _, key := range gi.Keys() {
		v, err := gi.GetItem(key)
		if err != nil {
			continue
		}
		gid, ok := v.(*objects.Int)
		if !ok {
			continue
		}
		n, _ := gid.Int64()
		g := int(n)
		if g < 0 || 2*g+1 >= len(md.locs) {
			continue
		}
		lo, hi := md.locs[2*g], md.locs[2*g+1]
		var val objects.Object
		if lo < 0 || hi < 0 {
			val = def
		} else {
			val = matchSlice(md, lo, hi)
		}
		_ = out.SetItem(key, val)
	}
	return out, nil
}

// matchStart implements SRE_Match.start([group]) with str support.
//
// CPython: Modules/_sre/sre.c:3042 _sre_SRE_Match_start_impl
func matchStart(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	inst, locs, _, err := matchInst(args, "start")
	if err != nil {
		return nil, err
	}
	g := 0
	if len(args) >= 2 {
		gg, gerr := resolveGroup(inst, args[1])
		if gerr != nil {
			return nil, gerr
		}
		g = gg
	}
	lo, _, err := groupSpan(locs, g)
	if err != nil {
		return nil, err
	}
	return objects.NewInt(int64(lo)), nil
}

// matchEnd implements SRE_Match.end([group]) with str support.
//
// CPython: Modules/_sre/sre.c:3059 _sre_SRE_Match_end_impl
func matchEnd(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	inst, locs, _, err := matchInst(args, "end")
	if err != nil {
		return nil, err
	}
	g := 0
	if len(args) >= 2 {
		gg, gerr := resolveGroup(inst, args[1])
		if gerr != nil {
			return nil, gerr
		}
		g = gg
	}
	_, hi, err := groupSpan(locs, g)
	if err != nil {
		return nil, err
	}
	return objects.NewInt(int64(hi)), nil
}

// matchSpan implements SRE_Match.span([group]) with str support.
//
// CPython: Modules/_sre/sre.c:3077 _sre_SRE_Match_span_impl
func matchSpan(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	inst, locs, _, err := matchInst(args, "span")
	if err != nil {
		return nil, err
	}
	g := 0
	if len(args) >= 2 {
		gg, gerr := resolveGroup(inst, args[1])
		if gerr != nil {
			return nil, gerr
		}
		g = gg
	}
	lo, hi, err := groupSpan(locs, g)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{
		objects.NewInt(int64(lo)),
		objects.NewInt(int64(hi)),
	}), nil
}

// matchExpand implements SRE_Match.expand(template). Parses the
// template with the same rules CPython's _parser.parse_template uses:
// `\g<name>`, `\g<N>`, `\N` (one to ninety-nine), `\\`, and recognized
// escape sequences `\n \r \t \a \b \f \v \0`. Backreferences to
// unmatched groups expand to the empty string, matching CPython.
//
// CPython: Lib/re/_parser.py:1003 parse_template
func matchExpand(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: expand() takes 2 arguments (self, template)")
	}
	inst, locs, src, err := matchInst(args, "expand")
	if err != nil {
		return nil, err
	}
	tmpl, terr := objects.Str(args[1])
	if terr != nil {
		return nil, fmt.Errorf("TypeError: expand() template must be str: %w", terr)
	}
	out, eerr := expandTemplate(inst, tmpl, locs, src)
	if eerr != nil {
		return nil, eerr
	}
	return objects.NewStr(out), nil
}

// expandTemplate is the template parser shared by Match.expand and
// (eventually, in phase 6) Pattern.sub. Returns the rendered string.
//
// CPython: Lib/re/_parser.py:1003 parse_template
// CPython: Modules/_sre/sre.c:2819 pattern_subx
func expandTemplate(inst *objects.Instance, tmpl string, locs []int, src string) (string, error) {
	var out strings.Builder
	out.Grow(len(tmpl))
	i := 0
	n := len(tmpl)
	for i < n {
		ch := tmpl[i]
		if ch != '\\' {
			out.WriteByte(ch)
			i++
			continue
		}
		// Lone trailing backslash: re.error in CPython.
		if i+1 >= n {
			return "", fmt.Errorf("re.error: bad escape (end of pattern) at position %d", i)
		}
		i++
		esc := tmpl[i]
		i++
		switch esc {
		case '\\':
			out.WriteByte('\\')
		case 'n':
			out.WriteByte('\n')
		case 'r':
			out.WriteByte('\r')
		case 't':
			out.WriteByte('\t')
		case 'a':
			out.WriteByte('\a')
		case 'b':
			out.WriteByte('\b')
		case 'f':
			out.WriteByte('\f')
		case 'v':
			out.WriteByte('\v')
		case '0':
			// \0 followed by 0-2 octal digits.
			val := 0
			for k := 0; k < 2 && i < n && isOctal(tmpl[i]); k++ {
				val = val*8 + int(tmpl[i]-'0')
				i++
			}
			out.WriteByte(byte(val))
		case 'g':
			// \g<name> or \g<N>
			if i >= n || tmpl[i] != '<' {
				return "", fmt.Errorf("re.error: missing < in group reference")
			}
			i++
			end := i
			for end < n && tmpl[end] != '>' {
				end++
			}
			if end >= n {
				return "", fmt.Errorf("re.error: missing > in group reference")
			}
			name := tmpl[i:end]
			i = end + 1
			if name == "" {
				return "", fmt.Errorf("re.error: missing group name")
			}
			g, gerr := lookupGroupForTemplate(inst, name)
			if gerr != nil {
				return "", gerr
			}
			if g < 0 || 2*g+1 >= len(locs) {
				return "", fmt.Errorf("IndexError: no such group")
			}
			lo, hi := locs[2*g], locs[2*g+1]
			if lo >= 0 {
				out.WriteString(src[lo:hi])
			}
		default:
			if esc >= '1' && esc <= '9' {
				// \N up to two digits (so \1..\99).
				num := int(esc - '0')
				if i < n && tmpl[i] >= '0' && tmpl[i] <= '9' {
					candidate := num*10 + int(tmpl[i]-'0')
					// Accept only if the resulting group exists; CPython
					// otherwise treats just the first digit.
					if 2*candidate+1 < len(locs) {
						num = candidate
						i++
					}
				}
				if 2*num+1 >= len(locs) {
					return "", fmt.Errorf("IndexError: no such group %d", num)
				}
				lo, hi := locs[2*num], locs[2*num+1]
				if lo >= 0 {
					out.WriteString(src[lo:hi])
				}
				continue
			}
			// Unknown escape: keep the backslash + char (CPython warns
			// then accepts).
			out.WriteByte('\\')
			out.WriteByte(esc)
		}
	}
	return out.String(), nil
}

func isOctal(b byte) bool { return b >= '0' && b <= '7' }

// lookupGroupForTemplate resolves a string from a `\g<...>` reference
// to a 0-based group number. Numeric strings parse as ints; otherwise
// the Pattern's groupindex map drives the lookup.
func lookupGroupForTemplate(inst *objects.Instance, name string) (int, error) {
	if n, err := strconv.Atoi(name); err == nil {
		return n, nil
	}
	reObj, err := inst.Dict().GetItem(objects.NewStr("re"))
	if err != nil {
		return -1, fmt.Errorf("IndexError: no such group")
	}
	patInst, ok := reObj.(*objects.Instance)
	if !ok {
		return -1, fmt.Errorf("IndexError: no such group")
	}
	giObj, err := patInst.Dict().GetItem(objects.NewStr("groupindex"))
	if err != nil {
		return -1, fmt.Errorf("IndexError: no such group")
	}
	gi, ok := giObj.(*objects.Dict)
	if !ok {
		return -1, fmt.Errorf("IndexError: no such group")
	}
	v, gerr := gi.GetItem(objects.NewStr(name))
	if gerr != nil {
		return -1, fmt.Errorf("IndexError: no such group")
	}
	gid, ok := v.(*objects.Int)
	if !ok {
		return -1, fmt.Errorf("IndexError: no such group")
	}
	n, _ := gid.Int64()
	return int(n), nil
}

// buildRegs builds the `regs` property: a tuple of (start, end) tuples
// covering group 0..groups.
//
// CPython: Modules/_sre/sre.c:3097 SRE_Match.regs
func buildRegs(locs []int) objects.Object {
	pairs := len(locs) / 2
	items := make([]objects.Object, pairs)
	for i := 0; i < pairs; i++ {
		items[i] = objects.NewTuple([]objects.Object{
			objects.NewInt(int64(locs[2*i])),
			objects.NewInt(int64(locs[2*i+1])),
		})
	}
	return objects.NewTuple(items)
}

// computeLastgroup resolves the `lastgroup` property: the name of the
// last matched group, or None. lastindex is the group number; the
// Pattern's `indexgroup` tuple maps gid -> name (or None).
//
// CPython: Modules/_sre/sre.c:3105 SRE_Match.lastgroup
func computeLastgroup(patInst objects.Object, lastindex int) objects.Object {
	if lastindex <= 0 {
		return objects.None()
	}
	pi, ok := patInst.(*objects.Instance)
	if !ok {
		return objects.None()
	}
	igObj, err := pi.Dict().GetItem(objects.NewStr("indexgroup"))
	if err != nil {
		return objects.None()
	}
	switch ig := igObj.(type) {
	case *objects.Tuple:
		if lastindex >= 0 && lastindex < ig.Len() {
			return ig.Item(lastindex)
		}
	case *objects.List:
		if lastindex >= 0 && lastindex < ig.Len() {
			return ig.Item(lastindex)
		}
	}
	return objects.None()
}
