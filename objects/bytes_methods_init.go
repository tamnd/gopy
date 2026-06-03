// Wires every entry in CPython 3.14 bytes_methods (Objects/bytesobject.c
// line 2665) and bytearray_methods (Objects/bytearrayobject.c line
// 2503) as a Python-visible descriptor on BytesType / ByteArrayType.
// The handler closures live in bytes_methods_descr.go and inspect
// args[0] to know whether to produce *Bytes or *ByteArray output, so
// the same closure can be installed on both types without forking.
//
// Mutating bytearray-only methods (append, extend, insert, pop,
// remove, clear, reverse, copy, __iadd__ / __imul__) are wired in
// bytearray_methods_init.go.

package objects

func init() {
	wireBytesLikeMethods(BytesType)
	wireBytesLikeMethods(ByteArrayType)
	wireBytesOnlyMethods(BytesType)
	wireBytearrayOnlyMethods(ByteArrayType)
}

// wireBytesLikeMethods installs the methods shared by bytes and
// bytearray. fromhex is wired as a classmethod that returns *Bytes
// (BytesType) or *ByteArray (ByteArrayType) depending on the
// receiver.
func wireBytesLikeMethods(t *Type) {
	produceByteArray := t == ByteArrayType
	SetTypeDescr(t, "capitalize", NewMethodDescr(t, "capitalize", caseTransformMethod("capitalize", bytesCapitalize)))
	SetTypeDescr(t, "lower", NewMethodDescr(t, "lower", caseTransformMethod("lower", bytesLower)))
	SetTypeDescr(t, "upper", NewMethodDescr(t, "upper", caseTransformMethod("upper", bytesUpper)))
	SetTypeDescr(t, "title", NewMethodDescr(t, "title", caseTransformMethod("title", bytesTitle)))
	SetTypeDescr(t, "swapcase", NewMethodDescr(t, "swapcase", caseTransformMethod("swapcase", bytesSwapCase)))

	SetTypeDescr(t, "isalpha", NewMethodDescr(t, "isalpha", classifierMethod("isalpha", bytesIsAlpha)))
	SetTypeDescr(t, "isalnum", NewMethodDescr(t, "isalnum", classifierMethod("isalnum", bytesIsAlnum)))
	SetTypeDescr(t, "isascii", NewMethodDescr(t, "isascii", classifierMethod("isascii", bytesIsASCII)))
	SetTypeDescr(t, "isdigit", NewMethodDescr(t, "isdigit", classifierMethod("isdigit", bytesIsDigit)))
	SetTypeDescr(t, "islower", NewMethodDescr(t, "islower", classifierMethod("islower", bytesIsLower)))
	SetTypeDescr(t, "isspace", NewMethodDescr(t, "isspace", classifierMethod("isspace", bytesIsSpace)))
	SetTypeDescr(t, "istitle", NewMethodDescr(t, "istitle", classifierMethod("istitle", bytesIsTitle)))
	SetTypeDescr(t, "isupper", NewMethodDescr(t, "isupper", classifierMethod("isupper", bytesIsUpper)))

	SetTypeDescr(t, "find", NewMethodDescr(t, "find", findMethod("find", false, false)))
	SetTypeDescr(t, "rfind", NewMethodDescr(t, "rfind", findMethod("rfind", true, false)))
	SetTypeDescr(t, "index", NewMethodDescr(t, "index", findMethod("index", false, true)))
	SetTypeDescr(t, "rindex", NewMethodDescr(t, "rindex", findMethod("rindex", true, true)))
	SetTypeDescr(t, "count", NewMethodDescr(t, "count", countMethod()))

	SetTypeDescr(t, "startswith", NewMethodDescr(t, "startswith", tailMatchMethod("startswith", false)))
	SetTypeDescr(t, "endswith", NewMethodDescr(t, "endswith", tailMatchMethod("endswith", true)))

	SetTypeDescr(t, "join", NewMethodDescr(t, "join", joinMethod()))

	SetTypeDescr(t, "strip", NewMethodDescr(t, "strip", stripMethod("strip", 'b')))
	SetTypeDescr(t, "lstrip", NewMethodDescr(t, "lstrip", stripMethod("lstrip", 'l')))
	SetTypeDescr(t, "rstrip", NewMethodDescr(t, "rstrip", stripMethod("rstrip", 'r')))

	SetTypeDescr(t, "replace", NewMethodDescr(t, "replace", replaceMethod()))
	SetTypeDescr(t, "removeprefix", NewMethodDescr(t, "removeprefix", removePrefixMethod()))
	SetTypeDescr(t, "removesuffix", NewMethodDescr(t, "removesuffix", removeSuffixMethod()))

	SetTypeDescr(t, "split", NewMethodDescr(t, "split", splitMethod("split", false)))
	SetTypeDescr(t, "rsplit", NewMethodDescr(t, "rsplit", splitMethod("rsplit", true)))
	SetTypeDescr(t, "splitlines", NewMethodDescr(t, "splitlines", splitLinesMethod()))

	SetTypeDescr(t, "partition", NewMethodDescr(t, "partition", partitionMethod("partition", false)))
	SetTypeDescr(t, "rpartition", NewMethodDescr(t, "rpartition", partitionMethod("rpartition", true)))

	SetTypeDescr(t, "translate", NewMethodDescr(t, "translate", translateMethod()))
	SetTypeDescr(t, "expandtabs", NewMethodDescr(t, "expandtabs", expandTabsMethod()))

	SetTypeDescr(t, "center", NewMethodDescr(t, "center", padMethod("center", 'c')))
	SetTypeDescr(t, "ljust", NewMethodDescr(t, "ljust", padMethod("ljust", 'l')))
	SetTypeDescr(t, "rjust", NewMethodDescr(t, "rjust", padMethod("rjust", 'r')))
	SetTypeDescr(t, "zfill", NewMethodDescr(t, "zfill", zfillMethod()))

	SetTypeDescr(t, "hex", NewMethodDescr(t, "hex", hexMethod()))
	SetTypeDescr(t, "decode", NewMethodDescr(t, "decode", decodeMethod()))

	SetTypeDescr(t, "fromhex", NewClassMethod(
		NewBuiltinFunction("fromhex", fromHexMethod(produceByteArray)),
	))
	SetTypeDescr(t, "maketrans", NewStaticMethod(
		NewBuiltinFunction("maketrans", makeTransMethod()),
	))
}

// wireBytesOnlyMethods is the bytes-only surface: __getnewargs__ and
// __bytes__. (bytearray gets its own pickle / mutation surface in
// wireBytearrayOnlyMethods.)
func wireBytesOnlyMethods(t *Type) {
	SetTypeDescr(t, "__getnewargs__", NewMethodDescr(t, "__getnewargs__", bytesGetNewArgsMethod()))
	SetTypeDescr(t, "__bytes__", NewMethodDescr(t, "__bytes__", bytesDunderBytesMethod()))
}

// wireBytearrayOnlyMethods installs the mutation methods on bytearray
// plus the bytearray-specific sticky surface (__alloc__, copy,
// append, extend, insert, pop, remove, clear, reverse).
func wireBytearrayOnlyMethods(t *Type) {
	SetTypeDescr(t, "append", NewMethodDescr(t, "append", bytearrayAppendMethod()))
	SetTypeDescr(t, "extend", NewMethodDescr(t, "extend", bytearrayExtendMethod()))
	SetTypeDescr(t, "insert", NewMethodDescr(t, "insert", bytearrayInsertMethod()))
	SetTypeDescr(t, "pop", NewMethodDescr(t, "pop", bytearrayPopMethod()))
	SetTypeDescr(t, "remove", NewMethodDescr(t, "remove", bytearrayRemoveMethod()))
	SetTypeDescr(t, "clear", NewMethodDescr(t, "clear", bytearrayClearMethod()))
	SetTypeDescr(t, "reverse", NewMethodDescr(t, "reverse", bytearrayReverseMethod()))
	SetTypeDescr(t, "copy", NewMethodDescr(t, "copy", bytearrayCopyMethod()))
	SetTypeDescr(t, "__alloc__", NewMethodDescr(t, "__alloc__", bytearrayAllocMethod()))
	SetTypeDescr(t, "resize", NewMethodDescr(t, "resize", bytearrayResizeMethod()))
}
