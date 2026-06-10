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
	// noArgs / methO tag the descriptor with the clinic flag so
	// methodDescrCheckArity formats the arity TypeError through
	// _PyObject_FunctionStr, yielding "bytes.upper() takes no arguments
	// (N given)" etc.
	//
	// CPython: Objects/bytesobject.c:2665 bytes_methods (stringlib rows are
	// METH_NOARGS); Objects/clinic/bytesobject.c.h for removeprefix/suffix.
	noArgs := func(name string, fn func(args []Object, kwargs map[string]Object) (Object, error)) {
		SetTypeDescr(t, name, NewMethodDescrConv(t, name, MethNoArgs, fn))
	}
	methO := func(name string, fn func(args []Object, kwargs map[string]Object) (Object, error)) {
		SetTypeDescr(t, name, NewMethodDescrConv(t, name, MethO, fn))
	}
	noArgs("capitalize", caseTransformMethod("capitalize", bytesCapitalize))
	noArgs("lower", caseTransformMethod("lower", bytesLower))
	noArgs("upper", caseTransformMethod("upper", bytesUpper))
	noArgs("title", caseTransformMethod("title", bytesTitle))
	noArgs("swapcase", caseTransformMethod("swapcase", bytesSwapCase))

	noArgs("isalpha", classifierMethod("isalpha", bytesIsAlpha))
	noArgs("isalnum", classifierMethod("isalnum", bytesIsAlnum))
	noArgs("isascii", classifierMethod("isascii", bytesIsASCII))
	noArgs("isdigit", classifierMethod("isdigit", bytesIsDigit))
	noArgs("islower", classifierMethod("islower", bytesIsLower))
	noArgs("isspace", classifierMethod("isspace", bytesIsSpace))
	noArgs("istitle", classifierMethod("istitle", bytesIsTitle))
	noArgs("isupper", classifierMethod("isupper", bytesIsUpper))

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
	methO("removeprefix", removePrefixMethod())
	methO("removesuffix", removeSuffixMethod())

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
	SetTypeDescr(t, "__getnewargs__", NewMethodDescrConv(t, "__getnewargs__", MethNoArgs, bytesGetNewArgsMethod()))
	SetTypeDescr(t, "__bytes__", NewMethodDescrConv(t, "__bytes__", MethNoArgs, bytesDunderBytesMethod()))
}

// wireBytearrayOnlyMethods installs the mutation methods on bytearray
// plus the bytearray-specific sticky surface (__alloc__, copy,
// append, extend, insert, pop, remove, clear, reverse).
func wireBytearrayOnlyMethods(t *Type) {
	// CPython: Objects/clinic/bytearrayobject.c.h — append/remove are
	// METH_O; clear/reverse/copy/__alloc__ are METH_NOARGS; insert/pop are
	// METH_FASTCALL and keep their hand-rolled messages.
	SetTypeDescr(t, "append", NewMethodDescrConv(t, "append", MethO, bytearrayAppendMethod()))
	SetTypeDescr(t, "extend", NewMethodDescrConv(t, "extend", MethO, bytearrayExtendMethod()))
	SetTypeDescr(t, "insert", NewMethodDescr(t, "insert", bytearrayInsertMethod()))
	SetTypeDescr(t, "pop", NewMethodDescr(t, "pop", bytearrayPopMethod()))
	SetTypeDescr(t, "remove", NewMethodDescrConv(t, "remove", MethO, bytearrayRemoveMethod()))
	SetTypeDescr(t, "clear", NewMethodDescrConv(t, "clear", MethNoArgs, bytearrayClearMethod()))
	SetTypeDescr(t, "reverse", NewMethodDescrConv(t, "reverse", MethNoArgs, bytearrayReverseMethod()))
	SetTypeDescr(t, "copy", NewMethodDescrConv(t, "copy", MethNoArgs, bytearrayCopyMethod()))
	SetTypeDescr(t, "__alloc__", NewMethodDescrConv(t, "__alloc__", MethNoArgs, bytearrayAllocMethod()))
	SetTypeDescr(t, "resize", NewMethodDescr(t, "resize", bytearrayResizeMethod()))
}
