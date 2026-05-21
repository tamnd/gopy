---
format: md
id: 1717_unicodedata_full_port
title: "1717. Modules/unicodedata.c full port"
sidebar_label: "1717. unicodedata"
sidebar_position: 1717
slug: /specs/1717-unicodedata
description: "1:1 Go port of CPython 3.14's Modules/unicodedata.c so import unicodedata resolves and normalize / category / east_asian_width / name / lookup / numeric helpers all return the same values CPython does. Unblocks the test_tokenize.py panel row in spec 1710 and removes the unicodedata import gap that ripples into stdlib/test/support/os_helper.py, stdlib/re/_parser.py, stdlib/urllib/parse.py, and stdlib/traceback.py."
---

## Rule

Same as 1704 / 1705 / 1708 / 1709. The deliverable is a Go file (or
files) under `module/unicodedata/` whose function list 1:1 covers
`Modules/unicodedata.c`. The Unicode Character Database tables live
in a generated Go file emitted from CPython 3.14's
`Modules/unicodedata_db.h` + `Modules/unicodename_db.h` so the
runtime carries no Python build dependency. Once this spec lands the
`stdlib/test/support/os_helper.py:30` `import unicodedata` resolves,
`unicodedata.normalize('NFD', ...)` returns CPython-equivalent
strings, and the `test_tokenize.py` panel row in spec 1710 advances
past the missing-module wall.

## Why this spec exists

CPython exposes `unicodedata` as a built-in C extension. The module
publishes:

- `normalize(form, str)` returning the NFC / NFD / NFKC / NFKD form.
  Required by `stdlib/test/support/os_helper.py:31`,
  `stdlib/urllib/parse.py:436`.
- `is_normalized(form, str)` returning True when str is already in
  the requested form.
- `category(char)` returning the two-letter general category ("Lu",
  "Ll", "Mn", ...).
- `bidirectional(char)` returning the bidi class.
- `combining(char)` returning the combining-class integer.
- `mirrored(char)` returning 1 if the character has Bidi_Mirrored.
- `east_asian_width(char)` returning the EAW class ("F", "H", "W",
  "Na", "A", "N"). Required by `stdlib/traceback.py:975`.
- `decimal(char[, default])`, `digit(char[, default])`,
  `numeric(char[, default])` returning the numeric value.
- `decomposition(char)` returning the canonical/compatibility
  decomposition as a hex string.
- `name(char[, default])` returning the Unicode 1.0/3.0 character
  name. Required transitively by `stdlib/re/_parser.py:349`'s
  `\N{NAME}` handling.
- `lookup(name)` returning the character for a name.
- `unidata_version` constant.
- The `UCD` type with a `ucd_3_2_0` pre-built instance for legacy
  callers (CPython keeps a Unicode 3.2 snapshot for IDNA).

`module/unicodedata/` does not exist yet. Every importer above falls
through to a `ModuleNotFoundError`, which is exactly what blocks the
spec 1710 `test_tokenize.py` panel row (the import chain runs
`unittest.mock` -> `pkgutil` -> ... -> `os_helper.py:30`).

## Sources of truth

- `Modules/unicodedata.c` (CPython 3.14.5): the function bodies.
- `Modules/unicodedata_db.h`: the auto-generated UCD records,
  decomposition tables, combining/quickcheck/EAW/numeric arrays.
- `Modules/unicodename_db.h`: the name-lookup phrasebook and trie.
- `Tools/unicode/makeunicodedata.py`: the generator that emits the
  two `_db.h` files from `Lib/test/cjkencodings/UnicodeData.txt`,
  `DerivedNormalizationProps.txt`, etc.

The gopy port uses Go's `unicode.RangeTable`s for the generic
category check fall-through (so basic `category('a') == 'Ll'`
matches without our own table), but every other property has to
come from the CPython UCD tables because Go's `unicode` package
doesn't expose combining classes, decomposition mappings, or
character names.

## Checklist

| Phase | Title | Status | Commit |
| ----- | ----- | ------ | ------ |
| P1 | `module/unicodedata/` skeleton + `imp.AppendInittab` + `stdlibinit` wiring + `unidata_version` constant | done | 106b099 |
| P2 | `Tools/unicodedata_go/` generator: parse CPython's UCD source headers and emit `module/unicodedata/data_gen.go` (records, decomposition, combining, EAW, numeric, quickcheck) | done | 106b099 |
| P3 | `normalize(form, str)` for NFC / NFD / NFKC / NFKD. Walks the decomposition tables + canonical ordering algorithm. Unblocks `os_helper.py:31`. | done | 106b099 |
| P4 | `is_normalized(form, str)` (quickcheck path + canonical verify) | done | 106b099 |
| P5 | `category`, `bidirectional`, `combining`, `mirrored`. `category` falls back to Go's `unicode` tables when the CPython record returns the default value | done | 106b099 |
| P6 | `east_asian_width(char)` returning "F/H/W/Na/A/N". Unblocks `stdlib/traceback.py:975` for non-ASCII traceback rendering | done | 106b099 |
| P7 | `decimal`, `digit`, `numeric` (with default arg semantics: raise ValueError or return default) | pending | - |
| P8 | Character-name generator: emit name trie from `unicodename_db.h`. Wire `name(char[, default])` and `lookup(name)`. Unblocks `\N{NAME}` in `re/_parser.py` | pending | - |
| P9 | `decomposition(char)` returning the hex form (`"<compat> 0020"` etc.) | done | 106b099 |
| P10 | `UCD` type + `ucd_3_2_0` legacy instance (Unicode 3.2 snapshot used by IDNA) | pending | - |
| P11 | Re-run the `test_tokenize.py` panel row from spec 1710, flip the row to either green or to the next out-of-scope blocker | pending | - |

## Phase notes

### P1: module skeleton

- New package `module/unicodedata/` with `module.go` carrying
  `init()` -> `imp.AppendInittab("unicodedata", buildModule)` and
  `buildModule()` returning an `*objects.Module` with the function
  table.
- Module-level constants: `unidata_version` (matches the
  `UNIDATA_VERSION` define from the CPython header we generate
  from).
- Blank-import line in `stdlibinit/registry.go`.
- Every function starts as a `not implemented` stub that returns a
  `NotImplementedError`. Subsequent phases swap the stubs for real
  implementations.

### P2: UCD data generator

- `Tools/unicodedata_go/main.go` parses CPython's pre-generated
  `Modules/unicodedata_db.h` (the C arrays + the index tables) and
  `Modules/unicodename_db.h`, then emits a Go file with the same
  data. Parsing the already-generated C tables (rather than
  re-running CPython's `makeunicodedata.py` against the UCD text
  files) keeps the generator small and matches the CPython runtime
  byte-for-byte.
- Output: `module/unicodedata/data_gen.go` plus optionally a
  `data_name_gen.go` split-out for the name table to keep file
  sizes manageable.
- Generator is run once and the output is checked in. The runtime
  build has no Python dependency.
- The `Tools/regen-unicodedata` Makefile target re-runs the
  generator against the current `$CPYTHON` checkout.

### P3: normalize

- Implements the Unicode Normalization Algorithm: full canonical /
  compatibility decomposition, canonical reordering (sort by
  Canonical_Combining_Class within each run of nonstarters), then
  canonical composition for NFC / NFKC.
- Backed by the decomposition tables emitted in P2 (CPython packs
  these into `decomp_data`, `decomp_index`, `decomp_index2`,
  `decomp_prefix`).
- `CPython: Modules/unicodedata.c:884 nfc_nfkc / nfd_nfkd_impl`

### P4: is_normalized

- Quickcheck NFC_QC / NFD_QC / NFKC_QC / NFKD_QC via the bitfield
  emitted in P2, then a verification pass that normalizes and
  string-compares.
- `CPython: Modules/unicodedata.c:819 QuickcheckResult`

### P5: category / bidirectional / combining / mirrored

- Read the per-codepoint `_PyUnicode_DatabaseRecord` index from the
  generated tables. Map the integer fields back to the string
  category / bidirectional names via the constant arrays CPython
  ships in `unicodedata_db.h` (`_PyUnicode_CategoryNames`,
  `_PyUnicode_BidirectionalNames`).
- Falls back to Go's `unicode.Categories` only as a sanity check;
  the per-codepoint record is authoritative.
- `CPython: Modules/unicodedata.c:131 unicodedata_UCD_category_impl`

### P6: east_asian_width

- Same record lookup, but returns the EAW two-letter string from
  `_PyUnicode_EastAsianWidthNames`.
- `CPython: Modules/unicodedata.c:290 unicodedata_UCD_east_asian_width_impl`

### P7: numeric

- `decimal(c)` returns the decimal digit value or raises
  `ValueError`. `digit(c)` returns the digit value (broader: tally
  marks etc.). `numeric(c)` returns the numeric value as a Go
  `float64` wrapped in a `*objects.Float`.
- Backed by the `_PyUnicode_DecimalDigitMapping`,
  `_PyUnicode_DigitMapping`, `_PyUnicode_NumericValues` arrays.
- `CPython: Modules/unicodedata.c:97 unicodedata_UCD_decimal_impl`

### P8: name / lookup

- Hardest table to port: CPython compresses character names with a
  word phrasebook + a hash trie keyed by the phrasebook indices.
  The generator emits both as Go data.
- `name(c)` walks the trie forward to reconstruct the name string;
  `lookup(name)` hashes the input and walks the inverse table.
- Falls back to the algorithmic ranges (Hangul syllables, CJK
  Unified Ideographs) hard-coded in CPython:
  `Modules/unicodedata.c:1004 hangul_syllables`,
  `Modules/unicodedata.c:1124 is_unified_ideograph`.
- `CPython: Modules/unicodedata.c:1513 unicodedata_UCD_name_impl`
- `CPython: Modules/unicodedata.c:1551 unicodedata_UCD_lookup_impl`

### P9: decomposition

- Returns the printable decomposition: `"<compat> 0020"` for
  compatibility forms, `"0061 0301"` for canonical. Empty string
  when the character has no decomposition.
- `CPython: Modules/unicodedata.c:319 unicodedata_UCD_decomposition_impl`

### P10: UCD type + ucd_3_2_0

- `unicodedata.UCD` is a type whose instances carry a frozen
  snapshot of the Unicode database. Most callers use the module-
  level helpers, which dispatch through the default UCD instance
  (Unicode 16.0). The legacy `ucd_3_2_0` instance backs IDNA's
  Unicode 3.2-stable behavior.
- For gopy, the 3.2 snapshot is generated alongside the main data
  table in P2 (Tools/unicodedata_go reads `unicodedata_db.h`'s
  change-record list and rolls codepoints back to their pre-3.2
  values).
- `CPython: Modules/unicodedata.c:86 DB_members`

### P11: panel re-run

- Rebuild gopy, run the corpus entry for `test_tokenize.py`. If
  unicodedata closes the only remaining gap the row flips green;
  if a further missing module surfaces (e.g. `_zoneinfo`), spec
  1710's panel row stays pending against the new blocker and a
  follow-up is filed.

## Out of scope

- Anything that needs `UnicodeData.txt` past Unicode 16.0. The
  generator stays pinned to whatever `$CPYTHON/Modules/unicodedata_db.h`
  ships at the gopy CPython tag.
- IDNA itself. `unicodedata.ucd_3_2_0` is the only thing IDNA needs
  from this module; the IDNA codec sits in `stdlib/encodings/idna.py`
  and is governed by a separate spec.

## Function-level audit

To be filled in alongside the implementation phases: each
`unicodedata.c` function gets a row with its CPython line, the
Go destination function, and a status tick.

| CPython function | CPython line | Go destination | Status |
| ---------------- | ------------ | -------------- | ------ |
| `_getrecord_ex`                           | 59   | `module/unicodedata/module.go` `getRecord`           | done |
| `unicodedata_UCD_decimal_impl`            | 132  | `module/unicodedata/stubs.go` `decimalBuiltin`       | stub |
| `unicodedata_UCD_digit_impl`              | 184  | `module/unicodedata/stubs.go` `digitBuiltin`         | stub |
| `unicodedata_UCD_numeric_impl`            | 218  | `module/unicodedata/stubs.go` `numericBuiltin`       | stub |
| `unicodedata_UCD_category_impl`           | 264  | `module/unicodedata/properties.go` `categoryBuiltin` | done |
| `unicodedata_UCD_bidirectional_impl`      | 291  | `module/unicodedata/properties.go` `bidirectionalBuiltin` | done |
| `unicodedata_UCD_combining_impl`          | 320  | `module/unicodedata/properties.go` `combiningBuiltin` | done |
| `unicodedata_UCD_mirrored_impl`           | 348  | `module/unicodedata/properties.go` `mirroredBuiltin` | done |
| `unicodedata_UCD_east_asian_width_impl`   | 375  | `module/unicodedata/properties.go` `eastAsianWidthBuiltin` | done |
| `unicodedata_UCD_decomposition_impl`      | 415  | `module/unicodedata/decomposition.go` `decompositionBuiltin` | done |
| `get_decomp_record`                       | 488  | `module/unicodedata/decomposition.go` `getDecompRecord` | done |
| `nfd_nfkd`                                | 514  | `module/unicodedata/normalize.go` `nfdNFKD`          | done |
| `find_nfc_index`                          | 649  | `module/unicodedata/normalize.go` `findNFCIndex`     | done |
| `nfc_nfkc`                                | 665  | `module/unicodedata/normalize.go` `nfcNFKC`          | done |
| `is_normalized_quickcheck`                | 820  | `module/unicodedata/normalize.go` `isNormalizedQuickcheck` | done |
| `unicodedata_UCD_is_normalized_impl`      | 885  | `module/unicodedata/normalize.go` `isNormalizedBuiltin` | done |
| `unicodedata_UCD_normalize_impl`          | 953  | `module/unicodedata/normalize.go` `normalizeBuiltin` | done |
| `unicodedata_UCD_name_impl`               | 1552 | `module/unicodedata/stubs.go` `nameBuiltin`          | stub |
| `unicodedata_UCD_lookup_impl`             | 1585 | `module/unicodedata/stubs.go` `lookupBuiltin`        | stub |
| `is_unified_ideograph`                    | 1124 | not yet ported                                       | pending |
| Hangul syllables decomposition            | 1004 | not yet ported                                       | pending |
| `PyInit_unicodedata`                      | 1734 | `module/unicodedata/module.go` `buildModule`         | done |
