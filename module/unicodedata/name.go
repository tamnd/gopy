// name / lookup. The DAWG decoder (_dawg_decode_*), inverse lookup
// (_inverse_dawg_lookup), forward lookup (_lookup_dawg_packed),
// _getucname and _getcode all port directly from
// Modules/unicodedata.c.
//
// CPython: Modules/unicodedata.c:1059 _dawg_decode_varint_unsigned
// CPython: Modules/unicodedata.c:1075 _dawg_match_edge
// CPython: Modules/unicodedata.c:1107 _dawg_decode_node
// CPython: Modules/unicodedata.c:1116 _dawg_node_is_final
// CPython: Modules/unicodedata.c:1124 _dawg_node_descendant_count
// CPython: Modules/unicodedata.c:1172 _dawg_decode_edge
// CPython: Modules/unicodedata.c:1196 _lookup_dawg_packed
// CPython: Modules/unicodedata.c:1242 _inverse_dawg_lookup
// CPython: Modules/unicodedata.c:1296 _getucname
// CPython: Modules/unicodedata.c:1378 find_syllable
// CPython: Modules/unicodedata.c:1411 parse_hex_code
// CPython: Modules/unicodedata.c:1441 _getcode
// CPython: Modules/unicodedata.c:1551 unicodedata_UCD_name_impl
// CPython: Modules/unicodedata.c:1584 unicodedata_UCD_lookup_impl

package unicodedata

import (
	"fmt"
	"strings"

	"github.com/tamnd/gopy/objects"
)

const nameMaxLen = 256

// isAlias reports whether code falls in the PUA range CPython uses
// for name aliases.
func isAlias(code rune) bool {
	return uint32(code) >= aliasesStart && uint32(code) < aliasesEnd
}

// isNamedSeq reports whether code falls in the PUA range CPython
// uses for named sequences.
func isNamedSeq(code rune) bool {
	return uint32(code) >= namedSeqStart && uint32(code) < namedSeqEnd
}

// dawgDecodeVarint reads the LEB128-style varint at packedNameDawg[index]
// and returns the new index plus the decoded value.
//
// CPython: Modules/unicodedata.c:1059 _dawg_decode_varint_unsigned
func dawgDecodeVarint(index int) (int, uint32) {
	var res uint32
	var shift uint
	for {
		b := packedNameDawg[index]
		res |= uint32(b&0x7f) << shift
		index++
		shift += 7
		if b&0x80 == 0 {
			return index, res
		}
	}
}

// dawgDecodeNode unpacks the node header (final bit + descendant
// count) and returns the offset of the first edge.
//
// CPython: Modules/unicodedata.c:1107 _dawg_decode_node
func dawgDecodeNode(offset int) (edgeOffset int, final bool) {
	off, num := dawgDecodeVarint(offset)
	return off, num&1 == 1
}

// dawgNodeIsFinal reports whether the node at offset accepts a name.
//
// CPython: Modules/unicodedata.c:1116 _dawg_node_is_final
func dawgNodeIsFinal(offset int) bool {
	_, num := dawgDecodeVarint(offset)
	return num&1 == 1
}

// dawgNodeDescendantCount returns the number of names the subtree
// rooted at offset enumerates.
//
// CPython: Modules/unicodedata.c:1124 _dawg_node_descendant_count
func dawgNodeDescendantCount(offset int) uint32 {
	_, num := dawgDecodeVarint(offset)
	return num >> 1
}

// dawgDecodeEdge parses one edge entry. Returns -1 for "final node
// with no edges", 0 for "this is not the last edge", or 1 for "this
// is the last edge". On success the size, label offset and target
// node offset are written into the out parameters.
//
// CPython: Modules/unicodedata.c:1172 _dawg_decode_edge
func dawgDecodeEdge(isFirstEdge bool, prevTarget int, edgeOffset int) (
	last int, size int, labelOffset int, targetOffset int,
) {
	off, num := dawgDecodeVarint(edgeOffset)
	if num == 0 && isFirstEdge {
		return -1, 0, 0, 0
	}
	lastEdge := num & 1
	num >>= 1
	lenIsOne := num & 1
	num >>= 1
	targetOffset = prevTarget + int(num)
	if lenIsOne == 1 {
		size = 1
	} else {
		size = int(packedNameDawg[off])
		off++
	}
	labelOffset = off
	return int(lastEdge), size, labelOffset, targetOffset
}

// dawgMatchEdge reports whether the edge's label matches the next
// `size` bytes of name starting at namepos. Returns 1 on match, 0
// on "not this edge", -1 on "no edge will match".
//
// CPython: Modules/unicodedata.c:1075 _dawg_match_edge
func dawgMatchEdge(name []byte, size int, labelOffset, namepos int) int {
	if size > 1 && namepos+size > len(name) {
		return 0
	}
	for i := 0; i < size; i++ {
		if namepos+i >= len(name) {
			return 0
		}
		if packedNameDawg[labelOffset+i] != asciiToUpper(name[namepos+i]) {
			if i > 0 {
				return -1
			}
			return 0
		}
	}
	return 1
}

// asciiToUpper folds an ASCII byte to its uppercase form. Unicode
// names are ASCII-only, so this matches CPython's Py_TOUPPER for
// the inputs we care about.
func asciiToUpper(b byte) byte {
	if b >= 'a' && b <= 'z' {
		return b - 32
	}
	return b
}

// lookupDawgPacked walks the DAWG looking for an exact match of
// name. Returns the rank (number of final nodes traversed) of the
// match, or -1 if no match exists.
//
// CPython: Modules/unicodedata.c:1196 _lookup_dawg_packed
func lookupDawgPacked(name []byte) int {
	stringpos := 0
	nodeOffset := 0
	result := uint32(0)
	for stringpos < len(name) {
		edgeOffset, final := dawgDecodeNode(nodeOffset)
		prevTarget := edgeOffset
		isFirstEdge := true
		for {
			lastEdge, size, labelOffset, targetOffset := dawgDecodeEdge(isFirstEdge, prevTarget, edgeOffset)
			if lastEdge == -1 {
				return -1
			}
			isFirstEdge = false
			prevTarget = targetOffset
			matched := dawgMatchEdge(name, size, labelOffset, stringpos)
			if matched == -1 {
				return -1
			}
			if matched == 1 {
				if final {
					result++
				}
				stringpos += size
				nodeOffset = targetOffset
				break
			}
			if lastEdge == 1 {
				return -1
			}
			result += dawgNodeDescendantCount(targetOffset)
			edgeOffset = labelOffset + size
		}
	}
	if dawgNodeIsFinal(nodeOffset) {
		return int(result)
	}
	return -1
}

// inverseDawgLookup walks the DAWG to recover the `pos`-th name.
// Returns the name and true on success, empty + false when the
// position is out of range.
//
// CPython: Modules/unicodedata.c:1242 _inverse_dawg_lookup
func inverseDawgLookup(pos uint32) (string, bool) {
	var buf strings.Builder
	nodeOffset := 0
	for {
		edgeOffset, final := dawgDecodeNode(nodeOffset)
		if final {
			if pos == 0 {
				return buf.String(), true
			}
			pos--
		}
		next, newPos, ok := inverseDawgWalkEdges(&buf, edgeOffset, pos)
		if !ok {
			return "", false
		}
		nodeOffset = next
		pos = newPos
	}
}

// inverseDawgWalkEdges follows one node's edge list looking for the
// edge whose subtree contains the `pos`-th remaining name. Appends
// the matching edge label to buf and returns the new node offset
// plus the remaining rank into that subtree.
func inverseDawgWalkEdges(buf *strings.Builder, edgeOffset int, pos uint32) (int, uint32, bool) {
	prevTarget := edgeOffset
	isFirstEdge := true
	for {
		lastEdge, size, labelOffset, targetOffset := dawgDecodeEdge(isFirstEdge, prevTarget, edgeOffset)
		if lastEdge == -1 {
			return 0, 0, false
		}
		isFirstEdge = false
		prevTarget = targetOffset
		desc := dawgNodeDescendantCount(targetOffset)
		if pos < desc {
			for i := 0; i < size; i++ {
				buf.WriteByte(packedNameDawg[labelOffset+i])
			}
			return targetOffset, pos, true
		}
		if lastEdge != 0 {
			return 0, 0, false
		}
		pos -= desc
		edgeOffset = labelOffset + size
	}
}

// findPrefixID looks up code in derivedNameRanges and returns the
// prefix index (0=HANGUL, 1=CJK, 2=TANGUT). Returns -1 when the
// code point is not in any derived range.
//
// CPython: Modules/unicodedata.c:1035 find_prefix_id
func findPrefixID(code rune) int {
	for _, r := range derivedNameRanges {
		if code < r.First {
			return -1
		}
		if code <= r.Last {
			return r.PrefixID
		}
	}
	return -1
}

// getUCName builds the Unicode name for code, returning "" + false
// when the code point has no name. withAliasAndSeq mirrors the
// CPython flag that lets internal callers see the PUA-encoded
// aliases and named sequences.
//
// CPython: Modules/unicodedata.c:1296 _getucname
func getUCName(self *UCD, code rune, withAliasAndSeq bool) (string, bool) {
	if code < 0 || code >= 0x110000 {
		return "", false
	}
	if !withAliasAndSeq && (isAlias(code) || isNamedSeq(code)) {
		return "", false
	}
	if self != nil {
		if isAlias(code) || isNamedSeq(code) {
			return "", false
		}
		if self.getRecord(code).CategoryChanged == 0 {
			return "", false
		}
	}

	prefixID := findPrefixID(code)
	if prefixID == 0 {
		sIndex := int(code - hangulSBase)
		l := sIndex / hangulNCount
		v := (sIndex % hangulNCount) / hangulTCount
		t := sIndex % hangulTCount
		return "HANGUL SYLLABLE " + hangulSyllables[l][0] + hangulSyllables[v][1] + hangulSyllables[t][2], true
	}
	if prefixID == 1 {
		return fmt.Sprintf("%s%04X", derivedNamePrefixes[prefixID], code), true
	}

	offset := int(dawgCodeToPosIndex1[code>>dawgPosShift])
	offset = int(dawgCodeToPosIndex2[(offset<<dawgPosShift)+(int(code)&((1<<dawgPosShift)-1))])
	if uint32(offset) == dawgPosNotFound {
		return "", false
	}
	return inverseDawgLookup(uint32(offset))
}

// findSyllable scans the Hangul-syllable table at the given column
// for the longest case-insensitive prefix of str. Returns the byte
// length consumed and the matching row index, or (0, -1) on no
// match.
//
// CPython: Modules/unicodedata.c:1378 find_syllable
func findSyllable(str string, count, column int) (matchedLen, pos int) {
	matchedLen = -1
	pos = -1
	for i := 0; i < count; i++ {
		s := hangulSyllables[i][column]
		l := len(s)
		if l <= matchedLen {
			continue
		}
		if l <= len(str) && strings.EqualFold(str[:l], s) {
			matchedLen = l
			pos = i
		}
	}
	if matchedLen == -1 {
		matchedLen = 0
	}
	return matchedLen, pos
}

// parseHexCode reads a 4- to 6-digit hex number, requiring a leading
// non-zero digit and a value at most 0x10FFFF. Returns -1 on failure.
//
// CPython: Modules/unicodedata.c:1411 parse_hex_code
func parseHexCode(name string) rune {
	if len(name) < 4 || len(name) > 6 {
		return -1
	}
	if name[0] == '0' {
		return -1
	}
	v := 0
	for i := 0; i < len(name); i++ {
		c := asciiToUpper(name[i])
		v *= 16
		switch {
		case c >= '0' && c <= '9':
			v += int(c - '0')
		case c >= 'A' && c <= 'F':
			v += int(c-'A') + 10
		default:
			return -1
		}
	}
	if v > 0x10FFFF {
		return -1
	}
	return rune(v)
}

// getCode resolves name to a code point. Returns -1 when no match.
// Aliases and named sequences come back as their PUA encoding;
// callers must run checkAliasAndSeq to map them to the resolved
// code point or sequence.
//
// CPython: Modules/unicodedata.c:1441 _getcode
func getCode(name string) rune {
	idx := -1
	prefixLen := 0
	for i, prefix := range derivedNamePrefixes {
		if len(name) >= len(prefix) && strings.EqualFold(name[:len(prefix)], prefix) {
			idx = i
			prefixLen = len(prefix)
			break
		}
	}

	if idx == 0 {
		pos := name[16:]
		consumed := 16
		l1, lIdx := findSyllable(pos, hangulLCount, 0)
		pos = pos[l1:]
		consumed += l1
		l2, vIdx := findSyllable(pos, hangulVCount, 1)
		pos = pos[l2:]
		consumed += l2
		l3, tIdx := findSyllable(pos, hangulTCount, 2)
		consumed += l3
		if lIdx != -1 && vIdx != -1 && tIdx != -1 && consumed == len(name) {
			return rune(hangulSBase + (lIdx*hangulVCount+vIdx)*hangulTCount + tIdx)
		}
		return -1
	}

	if idx > 0 && idx < len(derivedNamePrefixes) {
		v := parseHexCode(name[prefixLen:])
		if v < 0 {
			return -1
		}
		if findPrefixID(v) != idx {
			return -1
		}
		return v
	}

	pos := lookupDawgPacked([]byte(name))
	if pos < 0 {
		return -1
	}
	return rune(dawgPosToCode[pos])
}

// CPython: Modules/unicodedata.c:1551 unicodedata_UCD_name_impl
func nameImpl(self *UCD, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	r, def, err := argCharWithDefault("name", args)
	if err != nil {
		return nil, err
	}
	s, ok := getUCName(self, r, false)
	if !ok {
		if def != nil {
			return def, nil
		}
		return nil, fmt.Errorf("ValueError: no such name")
	}
	return objects.NewStr(s), nil
}

// CPython: Modules/unicodedata.c:1584 unicodedata_UCD_lookup_impl
func lookupImpl(self *UCD, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: lookup() takes exactly 1 argument (%d given)", len(args))
	}
	u, ok := args[0].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: lookup() argument must be str, not %s", args[0].Type().Name)
	}
	name := u.Value()
	if len(name) > nameMaxLen {
		return nil, fmt.Errorf("KeyError: name too long")
	}
	code := getCode(name)
	if code < 0 {
		return nil, fmt.Errorf("KeyError: undefined character name '%s'", name)
	}
	if self != nil && (isAlias(code) || isNamedSeq(code)) {
		return nil, fmt.Errorf("KeyError: undefined character name '%s'", name)
	}
	if isNamedSeq(code) {
		idx := uint32(code) - namedSeqStart
		seq := namedSequences[idx].Seq
		out := make([]rune, len(seq))
		for i, c := range seq {
			out[i] = rune(c)
		}
		return objects.NewStr(string(out)), nil
	}
	if isAlias(code) {
		code = rune(nameAliases[uint32(code)-aliasesStart])
	}
	return objects.NewStr(string(code)), nil
}

// Lookup is the pure-Go entry point that the parser's \N{NAME} escape
// decoder uses. It mirrors unicodedata.lookup(): aliases resolve to
// their target code point, named sequences expand to a multi-rune
// string. ok is false when the name is unknown so callers can format
// the SyntaxError text themselves.
//
// CPython: Modules/unicodedata.c:1584 unicodedata_UCD_lookup_impl
func Lookup(name string) (string, bool) {
	if name == "" || len(name) > nameMaxLen {
		return "", false
	}
	code := getCode(name)
	if code < 0 {
		return "", false
	}
	if isNamedSeq(code) {
		idx := uint32(code) - namedSeqStart
		seq := namedSequences[idx].Seq
		out := make([]rune, len(seq))
		for i, c := range seq {
			out[i] = rune(c)
		}
		return string(out), true
	}
	if isAlias(code) {
		code = rune(nameAliases[uint32(code)-aliasesStart])
	}
	return string(code), true
}

func nameBuiltin(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return nameImpl(nil, args, kwargs)
}

func lookupBuiltin(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return lookupImpl(nil, args, kwargs)
}
