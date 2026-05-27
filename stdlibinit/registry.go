// Package stdlibinit is the gopy equivalent of CPython's
// Modules/config.c.in. The C build generates that file with one
// extern declaration plus one _PyImport_Inittab[] row per built-in
// module; the linker then materializes the inittab at startup.
//
// gopy uses Go init() blocks instead: each module package
// (module/gc/, module/contextvars/, ...) calls imp.AppendInittab
// from its own init(). Those init blocks only run when their
// package is imported somewhere in the dependency graph. Without
// a central registration site, cmd/gopy/main.go would have to
// blank-import every module package by hand, and forgetting one
// means import gc silently raises ModuleNotFoundError at runtime.
//
// This package centralizes that registration: blank-importing
// stdlibinit pulls in every gopy module package, which forces every
// init() to run, which populates imp.Inittab. cmd/gopy/main.go
// blank-imports stdlibinit and nothing else has to change.
//
// CPython: Modules/config.c.in:26 _PyImport_Inittab[]
// CPython: Python/import.c:2403 _PyImport_FindBuiltin
package stdlibinit

import (
	// Built-in module: builtins. Registers itself via
	// module/builtins/module.go init(). Wraps builtins.Init() so
	// `import builtins` resolves in the stdlib without cmd/gopy startup.
	// CPython: Modules/builtinsmodule.c:3116 builtin_init
	_ "github.com/tamnd/gopy/module/builtins"

	// Built-in module: gc. Registers itself via module/gc/module.go
	// init().
	// CPython: Modules/config.c.in:47 {"gc", PyInit_gc}
	_ "github.com/tamnd/gopy/module/gc"

	// Built-in module: _contextvars. Registers itself via
	// module/contextvars/module.go init().
	// CPython: Modules/config.c.in:50 {"_contextvars", PyInit__contextvars}
	_ "github.com/tamnd/gopy/module/contextvars"

	// Built-in module: sys. Registers itself via module/sys/module.go
	// init().
	// CPython: Modules/config.c.in:42 {"sys", _PyImport_BuiltinSys}
	_ "github.com/tamnd/gopy/module/sys"

	// Built-in module: _io. Registers itself via module/io/module.go
	// init(). The current port covers StringIO; the rest of the
	// type family is stubbed so io.py's `from _io import (...)`
	// resolves at name-lookup time.
	// CPython: Modules/config.c.in:48 {"_io", PyInit__io}
	_ "github.com/tamnd/gopy/module/io"

	// Built-in module: _types. Re-exports the canonical type
	// singletons (FunctionType, MappingProxyType, SimpleNamespace,
	// ...) consumed by the vendored Lib/types.py.
	// CPython: Modules/Setup.bootstrap.in:28 _types _typesmodule.c
	_ "github.com/tamnd/gopy/module/types"

	// Built-in module: errno. Registers itself via
	// module/errno/module.go init(). Exposes the host platform's
	// POSIX errno constants plus the reverse `errorcode` dict.
	// CPython: Modules/config.c.in:46 {"errno", PyInit_errno}
	_ "github.com/tamnd/gopy/module/errno"

	// Built-in module: _functools. Registers itself via
	// module/_functools/module.go init(). Backs Lib/functools.py with
	// the partial type, the cmp_to_key key class, reduce, and the
	// _lru_cache_wrapper used by @lru_cache.
	// CPython: Modules/config.c.in:45 {"_functools", PyInit__functools}
	_ "github.com/tamnd/gopy/module/_functools"

	// Built-in module: _operator. Registers itself via
	// module/_operator/module.go init(). Backs Lib/operator.py with
	// the fast paths for arithmetic, comparisons, item access, plus
	// the itemgetter/attrgetter/methodcaller callable types.
	// CPython: Modules/config.c.in:44 {"_operator", PyInit__operator}
	_ "github.com/tamnd/gopy/module/_operator"

	// Built-in module: _warnings. Registers itself via
	// module/_warnings/module.go init(). 1:1 port of CPython's
	// Python/_warnings.c, backing the vendored Lib/warnings.py and
	// the C-level PyErr_Warn family.
	// CPython: Modules/config.c.in:51 {"_warnings", _PyWarnings_Init}
	_ "github.com/tamnd/gopy/module/_warnings"

	// Built-in module: _tokenize. Placeholder port; the TokenizerIter
	// constructor raises NotImplementedError until Parser/tokenizer.c
	// lands. Registering it is enough to satisfy `import tokenize` at
	// module load (only _tokenize.TokenizerIter() is called lazily).
	// CPython: Modules/config.c.in {"_tokenize", PyInit__tokenize}
	_ "github.com/tamnd/gopy/module/_tokenize"

	// Built-in module: time. Registers itself via
	// module/_time/module.go init(). The Go directory name carries an
	// underscore prefix to avoid colliding with Go's stdlib `time`
	// package; the registered Python name is the bare `time`.
	// CPython: Modules/config.c.in:43 {"time", PyInit_time}
	_ "github.com/tamnd/gopy/module/_time"

	// Built-in module: itertools. Registers itself via
	// module/_itertools/module.go init(). The Go directory carries the
	// underscore prefix; the registered Python module name is the bare
	// `itertools`. 1:1 port of Modules/itertoolsmodule.c.
	// CPython: Modules/config.c.in:46 {"itertools", PyInit_itertools}
	_ "github.com/tamnd/gopy/module/_itertools"

	// Built-in module: _weakref. Registers itself via
	// module/_weakref/module.go init(). Ports Modules/_weakref.c,
	// publishing ref / ProxyType / CallableProxyType plus the four
	// module functions getweakrefcount, _remove_dead_weakref,
	// getweakrefs, proxy.
	// CPython: Modules/config.c.in:49 {"_weakref", PyInit__weakref}
	_ "github.com/tamnd/gopy/module/_weakref"

	// Built-in module: _abc. Registers itself via module/_abc/module.go
	// init(). Ports Modules/_abc.c: ABC machinery backing Lib/abc.py.
	// CPython: Modules/config.c.in:44 {"_abc", PyInit__abc}
	_ "github.com/tamnd/gopy/module/_abc"

	// Built-in module: _collections. Registers itself via
	// module/_collections/module.go init(). Ports
	// Modules/_collectionsmodule.c: deque, defaultdict, _tuplegetter,
	// _deque_iterator, _deque_reverse_iterator, _count_elements.
	// CPython: Modules/config.c.in:46 {"_collections", PyInit__collections}
	_ "github.com/tamnd/gopy/module/_collections"

	// Built-in module: _thread. Registers itself via module/_thread/module.go
	// init(). Exposes goroutine identity, lock allocation, and new-thread
	// creation backing Lib/threading.py and reprlib.py.
	// CPython: Modules/_threadmodule.c:1 (module init)
	_ "github.com/tamnd/gopy/module/_thread"

	// Built-in module: _random. Registers itself via module/_random/module.go
	// init(). Backs Lib/random.py with the Mersenne Twister PRNG.
	// CPython: Modules/_randommodule.c:1 (module init)
	_ "github.com/tamnd/gopy/module/_random"

	// Built-in module: _struct. Registers itself via
	// module/_struct/module.go init(). Provides pack/unpack of binary
	// data per a format string; backs the Lib/struct.py wrapper.
	// CPython: Modules/config.c.in:52 {"_struct", PyInit__struct}
	_ "github.com/tamnd/gopy/module/_struct"

	// Built-in module: _json. Registers itself via
	// module/_json/module.go init(). Accelerates json.py with
	// scanstring and encode_basestring helpers.
	// CPython: Modules/config.c.in:53 {"_json", PyInit__json}
	_ "github.com/tamnd/gopy/module/_json"

	// Built-in module: _codecs. Registers itself via
	// module/_codecs/module.go init(). Exposes Python's codec lookup
	// infrastructure: lookup, encode, decode, and per-codec helpers for
	// UTF-8, ASCII, and Latin-1.
	// CPython: Modules/_codecsmodule.c:507 _codecs_exec
	_ "github.com/tamnd/gopy/module/_codecs"

	// Codec subsystem: CJK codecs. The package's init() registers a
	// SearchFunc with codecs.Register that resolves cp932, cp949,
	// euc_kr, euc_jp, shift_jis, and the jisx0213 family. Mirrors
	// CPython's per-language _codecs_kr / _codecs_jp / ... built-in
	// modules.
	// CPython: Modules/cjkcodecs/_codecs_kr.c BEGIN_CODECS_LIST
	// CPython: Modules/cjkcodecs/_codecs_jp.c BEGIN_CODECS_LIST
	_ "github.com/tamnd/gopy/codecs/cjkcodecs"

	// Built-in module: _csv. Registers itself via
	// module/_csv/module.go init(). Backs Lib/csv.py with the reader,
	// writer, Dialect, and quoting constants.
	// CPython: Modules/_csv.c:1423 _csv_exec
	_ "github.com/tamnd/gopy/module/_csv"

	// Built-in module: _elementtree. Registers itself via
	// module/_elementtree/module.go init(). C accelerator backing
	// xml.etree.ElementTree. Phase 1 publishes ParseError, Element,
	// and SubElement; the TreeBuilder + XMLParser + ElementPath
	// integration land in later phases.
	// CPython: Modules/_elementtree.c:4495 module_exec
	_ "github.com/tamnd/gopy/module/_elementtree"

	// Built-in module: _string. Registers itself via
	// module/_string/module.go init(). Exposes formatter_parser and
	// formatter_field_name_split, the two low-level helpers consumed by
	// Lib/string.py's Formatter class.
	// CPython: Modules/_string.c:368 PyInit__string
	_ "github.com/tamnd/gopy/module/_string"

	// Go-backed Python modules: not in CPython's config.c.in (those
	// are pure-Python in Lib/), but until the corresponding Lib/*.py
	// vendoring lands the import system needs something to satisfy
	// `import <name>` pulled in by unittest.
	// Built-in module: _sre. Registers itself via module/_sre/module.go
	// init(). Backs Lib/re/ with the compiled pattern engine. Go's
	// regexp/RE2 backend.
	// CPython: Modules/_sre/sre.c:1 (module init)
	_ "github.com/tamnd/gopy/module/_sre"

	// Built-in module: _suggestions. Registers itself via
	// module/_suggestions/module.go init(). Exposes _generate_suggestions
	// used by traceback._find_keyword_typos to rank keyword typos via
	// CPython-faithful Levenshtein distance (MOVE_COST=2, CASE_COST=1).
	// CPython: Modules/_suggestions.c:67 PyInit__suggestions
	_ "github.com/tamnd/gopy/module/_suggestions"

	// Built-in module: _heapq. Registers itself via
	// module/_heapq/module.go init(). Ports Modules/_heapqmodule.c:
	// heappush, heappop, heappushpop, heapreplace, heapify, nlargest, nsmallest.
	// CPython: Modules/_heapqmodule.c:540 heapq_exec
	_ "github.com/tamnd/gopy/module/_heapq"

	// Built-in module: _bisect. Registers itself via
	// module/_bisect/module.go init(). Ports Modules/_bisectmodule.c:
	// bisect_left, bisect_right, insort_left, insort_right and their aliases.
	// CPython: Modules/_bisectmodule.c:260 bisect_exec
	_ "github.com/tamnd/gopy/module/_bisect"

	// Built-in module: _stat. Registers itself via
	// module/_stat/module.go init(). Ports Modules/_stat.c:
	// mode-type masks, permission bits, UF_/SF_ flags, S_IS* helpers, filemode.
	// CPython: Modules/_stat.c:290 stat_exec
	_ "github.com/tamnd/gopy/module/_stat"

	// Built-in module: _opcode. Registers itself via
	// module/_opcode/module.go init(). Ports Modules/_opcode.c: the
	// has_* predicates over the opcode metadata flag table, the
	// intrinsic/special-method name tables, and the binary-op symbol
	// list consumed by Lib/opcode.py + Lib/dis.py.
	// CPython: Modules/_opcode.c:422 _opcode_exec
	_ "github.com/tamnd/gopy/module/_opcode"

	// Built-in module: _typing. Registers itself via
	// module/_typing/module.go init(). 1:1 port of
	// Modules/_typingmodule.c: re-exports TypeVar / ParamSpec /
	// TypeVarTuple / ParamSpecArgs / ParamSpecKwargs / Generic /
	// TypeAliasType / Union / NoDefault plus the _idfunc identity
	// accelerator so Lib/typing.py imports cleanly.
	// CPython: Modules/config.c.in {"_typing", PyInit__typing}
	_ "github.com/tamnd/gopy/module/_typing"

	// Pure-Python shim modules backed by stdlib vendored .py files.
	// Each registers itself via an init() that calls imp.AppendInittab.
	_ "github.com/tamnd/gopy/module/argparse"
	_ "github.com/tamnd/gopy/module/atexit"
	_ "github.com/tamnd/gopy/module/contextlib"
	_ "github.com/tamnd/gopy/module/dataclasses"
	_ "github.com/tamnd/gopy/module/fnmatch"
	_ "github.com/tamnd/gopy/module/functools"
	_ "github.com/tamnd/gopy/module/os"
	_ "github.com/tamnd/gopy/module/signal"
	_ "github.com/tamnd/gopy/module/socket"
	_ "github.com/tamnd/gopy/module/weakref"

	// Built-in module: math. Registers itself via module/math/module.go init().
	// CPython: Modules/mathmodule.c:1 (module init)
	_ "github.com/tamnd/gopy/module/math"

	// Built-in module: _posixsubprocess. Registers itself via
	// module/_posixsubprocess/module.go init(). Backs subprocess.py with
	// the low-level fork+exec primitive: spawn a child process, wire stdin/
	// stdout/stderr via fd pairs, and return the child PID.
	// CPython: Modules/_posixsubprocess.c:1333 _posixsubprocessmodule
	_ "github.com/tamnd/gopy/module/_posixsubprocess"

	// Built-in module: select. Registers itself via module/select/module.go
	// init(). Backs selectors.SelectSelector (and therefore subprocess
	// communicate) with select(2). The Go package name is selectmod
	// because "select" is a reserved Go keyword; the inittab name remains
	// the Python-visible "select".
	// CPython: Modules/selectmodule.c:2855 select_exec
	_ "github.com/tamnd/gopy/module/select"

	// Built-in module: _winapi. Registers itself via module/_winapi/
	// module.go init(). Exposes the integer constants stdlib/shutil.py
	// and stdlib/subprocess.py import at module top level on Windows.
	// CPython: Modules/_winapi.c:3023 _winapi_exec
	_ "github.com/tamnd/gopy/module/_winapi"

	// Built-in module: _hashlib. Registers itself via
	// module/_hashlib/module.go init(). Backs Lib/hashlib.py with the
	// HASH object type and openssl_* convenience constructors using
	// Go's standard library crypto packages.
	// CPython: Modules/_hashopenssl.c:2374 _hashlibmodule
	_ "github.com/tamnd/gopy/module/_hashlib"

	// Built-in module: _uuid. Registers itself via module/_uuid/module.go
	// init(). Backs Lib/uuid.py with thread-safe UUID byte generation via
	// crypto/rand.
	// CPython: Modules/_uuidmodule.c:52 _uuid_generate_time_safe_impl
	_ "github.com/tamnd/gopy/module/_uuid"

	// Built-in module: _datetime. Registers itself via
	// module/_datetime/module.go init(). Ports Modules/_datetimemodule.c:
	// date, time, datetime, timedelta, timezone types.
	// CPython: Modules/_datetimemodule.c:7078 datetime_exec
	_ "github.com/tamnd/gopy/module/_datetime"

	// Built-in module: zlib. Registers itself via module/zlib/module.go
	// init(). Exposes compress/decompress, crc32/adler32, and streaming
	// compressobj/decompressobj backed by Go's compress/zlib, compress/flate,
	// compress/gzip, and hash/crc32.
	// CPython: Modules/zlibmodule.c:77 zlib_compress_impl
	_ "github.com/tamnd/gopy/module/zlib"

	// Built-in module: _socket. Registers itself via module/_socket/module.go
	// init(). Backs Lib/socket.py with the low-level socket API.
	// CPython: Modules/socketmodule.c:4040 socket_getaddrinfo
	_ "github.com/tamnd/gopy/module/_socket"

	// Built-in module: _imp. Registers itself via module/_imp/module.go
	// init(). Exposes the slice of CPython's imp module consumed by
	// importlib._bootstrap_external: source_hash (keyed SipHash-1-3 over
	// the source buffer), pyc_magic_number_token (raw int form of the
	// .pyc header magic), and check_hash_based_pycs.
	// CPython: Python/import.c:4943 imp_module
	_ "github.com/tamnd/gopy/module/_imp"

	// Built-in module: marshal. Registers itself via module/marshal/module.go
	// init(). Backs importlib._bootstrap_external + py_compile with the
	// dumps / loads / dump / load entry points plus the version constant.
	// CPython: Python/marshal.c:1949 marshal_methods
	_ "github.com/tamnd/gopy/module/marshal"

	// Built-in module: unicodedata. Registration happens in the
	// init() block below instead of inside module/unicodedata
	// itself because parser/pegen needs to import unicodedata for
	// PEP 3131 NFKC identifier normalisation; pulling imp +
	// marshal + specialize + compile in transitively would create
	// a test-time import cycle through compile <- parser <-
	// parser/pegen <- unicodedata. Keeping unicodedata as a leaf
	// (objects-only) package breaks that cycle. Ports
	// Modules/unicodedata.c: normalize / is_normalized / category /
	// bidirectional / combining / mirrored / east_asian_width /
	// decomposition plus the unidata_version constant. Unblocks
	// stdlib/test/support/os_helper.py's `import unicodedata` and
	// `unicodedata.normalize('NFD', ...)` along the test_tokenize.py
	// panel import chain.
	// CPython: Modules/unicodedata.c:1733 PyInit_unicodedata
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/module/unicodedata"

	// Built-in module: array. Registers itself via module/array/array.go
	// init(). Ports Modules/arraymodule.c: the array.array type backing
	// numeric typed buffers (typecodes b/B/u/w/h/H/i/I/l/L/q/Q/f/d) plus
	// the module-level typecodes constant.
	// CPython: Modules/arraymodule.c:3225 array_modexec
	_ "github.com/tamnd/gopy/module/array"

	// Built-in module: _pickle. Registers itself via
	// module/_pickle/module.go init(). Publishes the full surface
	// pickle.py reaches for in its C-accelerator try block:
	// HIGHEST_PROTOCOL / DEFAULT_PROTOCOL, PickleError /
	// PicklingError / UnpicklingError, dump / dumps / load / loads,
	// and the Pickler / Unpickler classes. With these in place the
	// `from _pickle import (...)` at pickle.py:1888 resolves and
	// pickle.dumps / pickle.loads route through the Go encoder /
	// decoder on every call. PickleBuffer (out-of-band buffers) is
	// still deferred, pickle.py wraps that import in its own try /
	// except so its absence is fine.
	// CPython: Modules/_pickle.c:7700 _pickle_exec
	_ "github.com/tamnd/gopy/module/_pickle"
)

// init registers the handful of built-in modules whose own packages
// deliberately stay clear of the imp -> marshal -> specialize ->
// compile chain (so they can be imported from packages the compile
// tests reach into without creating an import cycle).
func init() {
	_ = imp.AppendInittab("unicodedata", unicodedata.BuildModule)
}
