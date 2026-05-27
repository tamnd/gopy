# Minimal stub for the _ast C extension. Provides the AST node classes
# that annotationlib.py and ast.py need. Real CPython gets these from a
# C extension built from Python.asdl; gopy has no _ast C module yet so
# we provide pure-Python equivalents.
#
# Each node class stores its constructor kwargs as instance attributes.
# The ast.unparse() writer only reads those fields, so the shape is
# enough for annotationlib's Format.STRING path.

PyCF_ONLY_AST = 0x0400
PyCF_OPTIMIZED_AST = 0x8400
PyCF_TYPE_COMMENTS = 0x1000
PyCF_ALLOW_TOP_LEVEL_AWAIT = 0x0200


class AST:
    _fields = ()
    _attributes = ()
    __match_args__ = ()

    def __init_subclass__(cls, **kwargs):
        super().__init_subclass__(**kwargs)
        # CPython: Python/Python-ast.c AST type init (match_args slot)
        cls.__match_args__ = cls._fields

    def __new__(cls, /, *args, **kwargs):
        # CPython: Python/Python-ast.c:5161 ast_type_init
        # cls is positional-only so that fields named 'cls' (e.g. MatchClass)
        # can be passed as kwargs without conflicting with __new__'s first arg.
        obj = object.__new__(cls)
        for attr in cls._attributes:
            if attr.startswith('end_'):
                setattr(obj, attr, None)
        return obj

    def __init__(self, *args, **kwargs):
        # CPython: Python/Python-ast.c:5161 ast_type_init
        import warnings
        import types as _types
        cls = type(self)
        try:
            fields = cls._fields
        except AttributeError:
            raise AttributeError(
                f"type object 'ast.{cls.__qualname__}' has no attribute '_fields'"
            ) from None
        numfields = len(fields)
        if len(args) > numfields:
            raise TypeError(
                f"{cls.__name__} constructor takes at most {numfields} "
                f"positional argument{'s' if numfields != 1 else ''}"
            )
        # Bind positional args and track how many fields were explicitly set.
        # CPython: Python/Python-ast.c:5195 positional arg loop
        npos = len(args)
        for i in range(npos):
            setattr(self, fields[i], args[i])
        # Bind kwargs; track which additional fields were covered.
        # CPython: Python/Python-ast.c:5213 kw iteration
        attrs = cls._attributes
        kw_fields_set = []
        non_str_key_error = None
        for k, v in kwargs.items():
            if k in fields:
                # check for duplicate with positional
                if k in fields[:npos]:
                    raise TypeError(
                        f"{cls.__name__} got multiple values for argument {k!r}"
                    )
                kw_fields_set.append(k)
            else:
                if k not in attrs:
                    # CPython: Python/Python-ast.c:5250 unexpected kwarg
                    # Gopy converts non-string dict keys to their str() repr.
                    # For valid identifier keys use repr() for quoting; for
                    # converted non-string keys use the raw string (no extra
                    # quotes) and record that TypeError must be raised.
                    if isinstance(k, str) and k.isidentifier():
                        k_display = repr(k)
                    else:
                        k_display = k
                        non_str_key_error = TypeError(
                            f"attribute name must be string, not the given argument"
                        )
                    warnings.warn(
                        f"{cls.__name__}.__init__ got an unexpected keyword argument {k_display}. "
                        f"Support for arbitrary keyword arguments is deprecated "
                        f"and will be removed in Python 3.15.",
                        DeprecationWarning,
                        stacklevel=2,
                    )
            setattr(self, k, v)
        if non_str_key_error is not None:
            raise non_str_key_error
        # Process fields not provided by the caller.
        # CPython: Python/Python-ast.c:5268 remaining_fields loop
        if npos < numfields or len(kw_fields_set) < numfields - npos:
            field_types = getattr(cls, '_field_types', None)
            if field_types is None:
                return
            for i in range(numfields):
                field = fields[i]
                if i < npos:
                    continue
                if field in kw_fields_set:
                    continue
                ft = field_types.get(field)
                if ft is None:
                    # CPython: Python/Python-ast.c:5294 field missing from _field_types
                    warnings.warn(
                        f"Field {field!r} is missing from {cls.__name__}._field_types. "
                        f"This will become an error in Python 3.15.",
                        DeprecationWarning,
                        stacklevel=2,
                    )
                elif isinstance(ft, _types.UnionType) and type(None) in ft.__args__:
                    # Optional field; class-level None default already set.
                    pass
                elif isinstance(ft, _types.GenericAlias) and ft.__origin__ is list:
                    # CPython: Python/Python-ast.c:5308 list field -> set []
                    setattr(self, field, [])
                elif ft is expr_context:
                    # CPython: Python/Python-ast.c:5320 expr_context -> Load()
                    setattr(self, field, Load())
                else:
                    # CPython: Python/Python-ast.c:5329 required field missing
                    warnings.warn(
                        f"{cls.__name__}.__init__ missing 1 required positional argument: {field!r}. "
                        f"This will become an error in Python 3.15.",
                        DeprecationWarning,
                        stacklevel=2,
                    )

    def __replace__(self, **changes):
        # CPython: Python/Python-ast.c:5493 ast_type_replace
        cls = type(self)
        fields = cls._fields
        attrs = cls._attributes
        for key in changes:
            if key not in fields and key not in attrs:
                # Use repr() for valid identifier keys; plain str for converted
                # non-string keys (like object()) that arrive as their str() repr.
                k_display = repr(key) if isinstance(key, str) and key.isidentifier() else key
                raise TypeError(
                    f"{cls.__name__}.__replace__ got an unexpected keyword "
                    f"argument {k_display}."
                )
        new_kw = {}
        missing = []
        for field in fields:
            if field in changes:
                new_kw[field] = changes[field]
            elif hasattr(self, field):
                new_kw[field] = getattr(self, field)
            else:
                missing.append(field)
        if missing:
            n = len(missing)
            names = ', '.join(repr(f) for f in missing)
            plural = '' if n == 1 else 's'
            raise TypeError(
                f"{cls.__name__}.__replace__ missing {n} keyword "
                f"argument{plural}: {names}."
            )
        for attr in attrs:
            if attr in changes:
                new_kw[attr] = changes[attr]
            elif hasattr(self, attr):
                new_kw[attr] = getattr(self, attr)
        return cls(**new_kw)

    def __reduce__(self):
        # CPython: Python/Python-ast.c:5356 ast_type_reduce
        # Pass all fields as positional None-args so unpickling does not
        # trigger DeprecationWarnings for missing required fields.
        cls = type(self)
        fields = cls._fields
        state = {}
        for field in fields:
            if hasattr(self, field):
                state[field] = getattr(self, field)
        for attr in cls._attributes:
            if hasattr(self, attr):
                state[attr] = getattr(self, attr)
        return (cls, tuple(None for _ in fields), state)

    def __setstate__(self, state):
        # CPython: Python/Python-ast.c:5440 ast_type_setstate
        for k, v in state.items():
            setattr(self, k, v)

    def __repr__(self):
        # CPython: Python/Python-ast.c:5968 ast_repr -> ast_repr_max_depth(self, 3)
        return _ast_repr(self, 3)


# _repr_running guards against circular-reference loops; mirrors Py_ReprEnter.
# CPython: Python/Python-ast.c:5856 Py_ReprEnter
_repr_running = set()


def _ast_repr(node, depth):
    # CPython: Python/Python-ast.c:5845 ast_repr_max_depth
    if depth <= 0:
        return f'{type(node).__name__}(...)'
    node_id = id(node)
    if node_id in _repr_running:
        return f'{type(node).__name__}(...)'
    _repr_running.add(node_id)
    try:
        parts = []
        for f in node._fields:
            if not hasattr(node, f):
                continue
            v = getattr(node, f)
            if isinstance(v, list):
                vr = _ast_repr_list(v, depth)
            elif isinstance(v, AST):
                vr = _ast_repr(v, depth - 1)
            else:
                vr = _safe_repr(v)
            parts.append(f'{f}={vr}')
        return f'{type(node).__name__}({", ".join(parts)})'
    finally:
        _repr_running.discard(node_id)


def _ast_repr_list(lst, depth):
    # CPython: Python/Python-ast.c:5758 ast_repr_list
    # Only show first and last elements for lists with >2 items.
    length = len(lst)
    if length == 0:
        return '[]'

    def item_repr(item):
        if isinstance(item, AST):
            return _ast_repr(item, depth - 1)
        return _safe_repr(item)

    if length == 1:
        return f'[{item_repr(lst[0])}]'
    if length == 2:
        return f'[{item_repr(lst[0])}, {item_repr(lst[1])}]'
    return f'[{item_repr(lst[0])}, ..., {item_repr(lst[-1])}]'


def _safe_repr(v):
    # Mirror CPython's integer-to-string conversion size limit (4300 digits).
    # CPython: Lib/test/test_ast.py test_repr_large_input_crash
    if isinstance(v, int) and not isinstance(v, bool):
        # 14284 bits ≈ 4300 decimal digits (log2(10^4300))
        if v.bit_length() > 14284:
            raise ValueError(
                f"Exceeds the limit (4300 digits) for integer string conversion; "
                f"use sys.set_int_max_str_digits() to increase the limit"
            )
    return repr(v)


# -- expression context --

class expr_context(AST):
    _attributes = ()
class Load(expr_context): _fields = ()
class Store(expr_context): _fields = ()
class Del(expr_context): _fields = ()

# -- operators --

class operator(AST):
    _attributes = ()
class Add(operator): _fields = ()
class Sub(operator): _fields = ()
class Mult(operator): _fields = ()
class MatMult(operator): _fields = ()
class Div(operator): _fields = ()
class Mod(operator): _fields = ()
class Pow(operator): _fields = ()
class LShift(operator): _fields = ()
class RShift(operator): _fields = ()
class BitOr(operator): _fields = ()
class BitXor(operator): _fields = ()
class BitAnd(operator): _fields = ()
class FloorDiv(operator): _fields = ()

# -- unary operators --

class unaryop(AST):
    _attributes = ()
class Invert(unaryop): _fields = ()
class Not(unaryop): _fields = ()
class UAdd(unaryop): _fields = ()
class USub(unaryop): _fields = ()

# -- comparison operators --

class cmpop(AST):
    _attributes = ()
class Eq(cmpop): _fields = ()
class NotEq(cmpop): _fields = ()
class Lt(cmpop): _fields = ()
class LtE(cmpop): _fields = ()
class Gt(cmpop): _fields = ()
class GtE(cmpop): _fields = ()
class Is(cmpop): _fields = ()
class IsNot(cmpop): _fields = ()
class In(cmpop): _fields = ()
class NotIn(cmpop): _fields = ()

# -- boolean operators --

class boolop(AST):
    _attributes = ()
class And(boolop): _fields = ()
class Or(boolop): _fields = ()

# -- expression nodes --

class expr(AST):
    _attributes = ('lineno', 'col_offset', 'end_lineno', 'end_col_offset')
    end_lineno = None
    end_col_offset = None

class Name(expr):
    _fields = ('id', 'ctx')
class Constant(expr):
    _fields = ('value', 'kind')

class Attribute(expr):
    _fields = ('value', 'attr', 'ctx')
class Subscript(expr):
    _fields = ('value', 'slice', 'ctx')
class Starred(expr):
    _fields = ('value', 'ctx')
class List(expr):
    _fields = ('elts', 'ctx')
class Tuple(expr):
    _fields = ('elts', 'ctx')
class Set(expr):
    _fields = ('elts',)
class Dict(expr):
    _fields = ('keys', 'values')
class BinOp(expr):
    _fields = ('left', 'op', 'right')

class UnaryOp(expr):
    _fields = ('op', 'operand')

class BoolOp(expr):
    _fields = ('op', 'values')
class Compare(expr):
    _fields = ('left', 'ops', 'comparators')
class Call(expr):
    _fields = ('func', 'args', 'keywords')
class IfExp(expr):
    _fields = ('test', 'body', 'orelse')

class Slice(expr):
    _fields = ('lower', 'upper', 'step')

class Lambda(expr):
    _fields = ('args', 'body')

class NamedExpr(expr):
    _fields = ('target', 'value')

# -- misc nodes --

class keyword(AST):
    _fields = ('arg', 'value')
    _attributes = ('lineno', 'col_offset', 'end_lineno', 'end_col_offset')
    end_lineno = None
    end_col_offset = None
class arg(AST):
    _fields = ('arg', 'annotation', 'type_comment')
    _attributes = ('lineno', 'col_offset', 'end_lineno', 'end_col_offset')
    end_lineno = None
    end_col_offset = None
class arguments(AST):
    _fields = ('posonlyargs', 'args', 'vararg', 'kwonlyargs',
               'kw_defaults', 'kwarg', 'defaults')
class mod(AST):
    _attributes = ()

class stmt(AST):
    _attributes = ('lineno', 'col_offset', 'end_lineno', 'end_col_offset')
    end_lineno = None
    end_col_offset = None

class Module(mod):
    _fields = ('body', 'type_ignores')
class Expression(mod):
    _fields = ('body',)
class Interactive(mod):
    _fields = ('body',)
class FunctionType(mod):
    _fields = ('argtypes', 'returns')
class FunctionDef(stmt):
    _fields = ('name', 'args', 'body', 'decorator_list', 'returns',
               'type_comment', 'type_params')
class AsyncFunctionDef(stmt):
    _fields = ('name', 'args', 'body', 'decorator_list', 'returns',
               'type_comment', 'type_params')
class ClassDef(stmt):
    _fields = ('name', 'bases', 'keywords', 'body', 'decorator_list',
               'type_params')
class Return(stmt):
    _fields = ('value',)

class Assign(stmt):
    _fields = ('targets', 'value', 'type_comment')

class AugAssign(stmt):
    _fields = ('target', 'op', 'value')
class AnnAssign(stmt):
    _fields = ('target', 'annotation', 'value', 'simple')

class Expr(stmt):
    _fields = ('value',)

class Pass(stmt): _fields = ()
class Break(stmt): _fields = ()
class Continue(stmt): _fields = ()

class Raise(stmt):
    _fields = ('exc', 'cause')

class If(stmt):
    _fields = ('test', 'body', 'orelse')
class For(stmt):
    _fields = ('target', 'iter', 'body', 'orelse', 'type_comment')
class While(stmt):
    _fields = ('test', 'body', 'orelse')
class With(stmt):
    _fields = ('items', 'body', 'type_comment')
class AsyncFor(stmt):
    _fields = ('target', 'iter', 'body', 'orelse', 'type_comment')
class AsyncWith(stmt):
    _fields = ('items', 'body', 'type_comment')
class withitem(AST):
    _fields = ('context_expr', 'optional_vars')
class Import(stmt):
    _fields = ('names',)
class ImportFrom(stmt):
    _fields = ('module', 'names', 'level')
class alias(AST):
    _fields = ('name', 'asname')
    _attributes = ('lineno', 'col_offset', 'end_lineno', 'end_col_offset')
    end_lineno = None
    end_col_offset = None
class Global(stmt):
    _fields = ('names',)
class Nonlocal(stmt):
    _fields = ('names',)
class Delete(stmt):
    _fields = ('targets',)
class Assert(stmt):
    _fields = ('test', 'msg')

class Match(stmt):
    _fields = ('subject', 'cases')
class match_case(AST):
    _fields = ('pattern', 'guard', 'body')
class pattern(AST):
    _attributes = ('lineno', 'col_offset', 'end_lineno', 'end_col_offset')
    end_lineno = None
    end_col_offset = None

class excepthandler(AST):
    _attributes = ('lineno', 'col_offset', 'end_lineno', 'end_col_offset')
    end_lineno = None
    end_col_offset = None

class type_ignore(AST):
    _attributes = ()
class type_param(AST):
    _attributes = ('lineno', 'col_offset', 'end_lineno', 'end_col_offset')
    end_lineno = None
    end_col_offset = None

class MatchValue(pattern):
    _fields = ('value',)
class MatchSingleton(pattern):
    _fields = ('value',)
class MatchSequence(pattern):
    _fields = ('patterns',)
class MatchMapping(pattern):
    _fields = ('keys', 'patterns', 'rest')
class MatchClass(pattern):
    _fields = ('cls', 'patterns', 'kwd_attrs', 'kwd_patterns')
class MatchStar(pattern):
    _fields = ('name',)
class MatchAs(pattern):
    _fields = ('pattern', 'name')
class MatchOr(pattern):
    _fields = ('patterns',)
class Try(stmt):
    _fields = ('body', 'handlers', 'orelse', 'finalbody')
class TryStar(stmt):
    _fields = ('body', 'handlers', 'orelse', 'finalbody')
class ExceptHandler(excepthandler):
    _fields = ('type', 'name', 'body')
class TypeIgnore(type_ignore):
    _fields = ('lineno', 'tag')

class TypeVar(type_param):
    _fields = ('name', 'bound', 'default_value')
class TypeVarTuple(type_param):
    _fields = ('name', 'default_value')
class ParamSpec(type_param):
    _fields = ('name', 'default_value')
class TypeAlias(stmt):
    _fields = ('name', 'type_params', 'value')

# Template string nodes (PEP 750)
class TemplateStr(expr):
    _fields = ('values',)
class Interpolation(expr):
    _fields = ('value', 'str', 'conversion', 'format_spec')
class JoinedStr(expr):
    _fields = ('values',)
class FormattedValue(expr):
    _fields = ('value', 'conversion', 'format_spec')
class comprehension(AST):
    _fields = ('target', 'iter', 'ifs', 'is_async')
class ListComp(expr):
    _fields = ('elt', 'generators')
class SetComp(expr):
    _fields = ('elt', 'generators')
class GeneratorExp(expr):
    _fields = ('elt', 'generators')
class DictComp(expr):
    _fields = ('key', 'value', 'generators')
class Await(expr):
    _fields = ('value',)

class Yield(expr):
    _fields = ('value',)

class YieldFrom(expr):
    _fields = ('value',)


# _field_types: mapping from field name to expected type.
# CPython: Python/Python-ast.c (auto-generated, one dict per node type)
def _setup_field_types():
    Module._field_types = {'body': list[stmt], 'type_ignores': list[type_ignore]}
    Expression._field_types = {'body': expr}
    Interactive._field_types = {'body': list[stmt]}
    FunctionType._field_types = {'argtypes': list[expr], 'returns': expr}
    FunctionDef._field_types = {
        'name': str, 'args': arguments, 'body': list[stmt],
        'decorator_list': list[expr], 'returns': expr | None,
        'type_comment': str | None, 'type_params': list[type_param],
    }
    AsyncFunctionDef._field_types = {
        'name': str, 'args': arguments, 'body': list[stmt],
        'decorator_list': list[expr], 'returns': expr | None,
        'type_comment': str | None, 'type_params': list[type_param],
    }
    ClassDef._field_types = {
        'name': str, 'bases': list[expr], 'keywords': list[keyword],
        'body': list[stmt], 'decorator_list': list[expr],
        'type_params': list[type_param],
    }
    Return._field_types = {'value': expr | None}
    Delete._field_types = {'targets': list[expr]}
    Assign._field_types = {'targets': list[expr], 'value': expr, 'type_comment': str | None}
    TypeAlias._field_types = {'name': expr, 'type_params': list[type_param], 'value': expr}
    AugAssign._field_types = {'target': expr, 'op': operator, 'value': expr}
    AnnAssign._field_types = {'target': expr, 'annotation': expr, 'value': expr | None, 'simple': int}
    For._field_types = {
        'target': expr, 'iter': expr, 'body': list[stmt],
        'orelse': list[stmt], 'type_comment': str | None,
    }
    AsyncFor._field_types = {
        'target': expr, 'iter': expr, 'body': list[stmt],
        'orelse': list[stmt], 'type_comment': str | None,
    }
    While._field_types = {'test': expr, 'body': list[stmt], 'orelse': list[stmt]}
    If._field_types = {'test': expr, 'body': list[stmt], 'orelse': list[stmt]}
    With._field_types = {'items': list[withitem], 'body': list[stmt], 'type_comment': str | None}
    AsyncWith._field_types = {'items': list[withitem], 'body': list[stmt], 'type_comment': str | None}
    Match._field_types = {'subject': expr, 'cases': list[match_case]}
    Raise._field_types = {'exc': expr | None, 'cause': expr | None}
    Try._field_types = {
        'body': list[stmt], 'handlers': list[excepthandler],
        'orelse': list[stmt], 'finalbody': list[stmt],
    }
    TryStar._field_types = {
        'body': list[stmt], 'handlers': list[excepthandler],
        'orelse': list[stmt], 'finalbody': list[stmt],
    }
    Assert._field_types = {'test': expr, 'msg': expr | None}
    Import._field_types = {'names': list[alias]}
    ImportFrom._field_types = {'module': str | None, 'names': list[alias], 'level': int | None}
    Global._field_types = {'names': list[str]}
    Nonlocal._field_types = {'names': list[str]}
    Expr._field_types = {'value': expr}
    Pass._field_types = {}
    Break._field_types = {}
    Continue._field_types = {}
    BoolOp._field_types = {'op': boolop, 'values': list[expr]}
    NamedExpr._field_types = {'target': expr, 'value': expr}
    BinOp._field_types = {'left': expr, 'op': operator, 'right': expr}
    UnaryOp._field_types = {'op': unaryop, 'operand': expr}
    Lambda._field_types = {'args': arguments, 'body': expr}
    IfExp._field_types = {'test': expr, 'body': expr, 'orelse': expr}
    Dict._field_types = {'keys': list[expr], 'values': list[expr]}
    Set._field_types = {'elts': list[expr]}
    ListComp._field_types = {'elt': expr, 'generators': list[comprehension]}
    SetComp._field_types = {'elt': expr, 'generators': list[comprehension]}
    DictComp._field_types = {'key': expr, 'value': expr, 'generators': list[comprehension]}
    GeneratorExp._field_types = {'elt': expr, 'generators': list[comprehension]}
    Await._field_types = {'value': expr}
    Yield._field_types = {'value': expr | None}
    YieldFrom._field_types = {'value': expr}
    Compare._field_types = {'left': expr, 'ops': list[cmpop], 'comparators': list[expr]}
    Call._field_types = {'func': expr, 'args': list[expr], 'keywords': list[keyword]}
    FormattedValue._field_types = {'value': expr, 'conversion': int, 'format_spec': expr | None}
    JoinedStr._field_types = {'values': list[expr]}
    Constant._field_types = {'value': object, 'kind': str | None}
    Attribute._field_types = {'value': expr, 'attr': str, 'ctx': expr_context}
    Subscript._field_types = {'value': expr, 'slice': expr, 'ctx': expr_context}
    Starred._field_types = {'value': expr, 'ctx': expr_context}
    Name._field_types = {'id': str, 'ctx': expr_context}
    List._field_types = {'elts': list[expr], 'ctx': expr_context}
    Tuple._field_types = {'elts': list[expr], 'ctx': expr_context}
    Slice._field_types = {'lower': expr | None, 'upper': expr | None, 'step': expr | None}
    arg._field_types = {'arg': str, 'annotation': expr | None, 'type_comment': str | None}
    arguments._field_types = {
        'posonlyargs': list[arg], 'args': list[arg], 'vararg': arg | None,
        'kwonlyargs': list[arg], 'kw_defaults': list[expr],
        'kwarg': arg | None, 'defaults': list[expr],
    }
    keyword._field_types = {'arg': str | None, 'value': expr}
    withitem._field_types = {'context_expr': expr, 'optional_vars': expr | None}
    comprehension._field_types = {'target': expr, 'iter': expr, 'ifs': list[expr], 'is_async': int}
    alias._field_types = {'name': str, 'asname': str | None}
    ExceptHandler._field_types = {'type': expr | None, 'name': str | None, 'body': list[stmt]}
    TypeIgnore._field_types = {'lineno': int, 'tag': str}
    TypeVar._field_types = {'name': str, 'bound': expr | None, 'default_value': expr | None}
    TypeVarTuple._field_types = {'name': str, 'default_value': expr | None}
    ParamSpec._field_types = {'name': str, 'default_value': expr | None}
    match_case._field_types = {'pattern': pattern, 'guard': expr | None, 'body': list[stmt]}
    MatchValue._field_types = {'value': expr}
    MatchSingleton._field_types = {'value': object}
    MatchSequence._field_types = {'patterns': list[pattern]}
    MatchMapping._field_types = {'keys': list[expr], 'patterns': list[pattern], 'rest': str | None}
    MatchClass._field_types = {
        'cls': expr, 'patterns': list[pattern],
        'kwd_attrs': list[str], 'kwd_patterns': list[pattern],
    }
    MatchStar._field_types = {'name': str | None}
    MatchAs._field_types = {'pattern': pattern | None, 'name': str | None}
    MatchOr._field_types = {'patterns': list[pattern]}
    TemplateStr._field_types = {'values': list[expr]}
    Interpolation._field_types = {'value': expr, 'str': object, 'conversion': int, 'format_spec': expr | None}

    # Sync _field_types -> __annotations__ for all AST subclasses that
    # have _field_types defined directly in their own __dict__.
    # CPython: Python/Python-ast.c (C types expose __annotations__ via _field_types)
    for _cls in globals().values():
        if isinstance(_cls, type) and issubclass(_cls, AST):
            _ft = _cls.__dict__.get('_field_types')
            if _ft is not None:
                _cls.__annotations__ = _ft

_setup_field_types()


# Class-level None defaults mirror CPython's Python-ast.c type slots.
# ast.dump() uses `getattr(cls, field, ...) is None` to decide whether
# to omit a field that holds None; without these the optional fields
# appear in dump output even when they should be hidden.
#
# CPython: Python/Python-ast.c (auto-generated slot defaults)
def _setup_none_defaults():
    alias.asname = None
    AnnAssign.value = None
    arg.annotation = None
    arg.type_comment = None
    arguments.kwarg = None
    arguments.vararg = None
    Assert.msg = None
    Assign.type_comment = None
    AsyncFor.type_comment = None
    AsyncFunctionDef.returns = None
    AsyncFunctionDef.type_comment = None
    AsyncWith.type_comment = None
    Constant.kind = None
    ExceptHandler.name = None
    ExceptHandler.type = None
    For.type_comment = None
    FormattedValue.format_spec = None
    FunctionDef.returns = None
    FunctionDef.type_comment = None
    ImportFrom.level = None
    ImportFrom.module = None
    Interpolation.format_spec = None
    keyword.arg = None
    match_case.guard = None
    MatchAs.name = None
    MatchAs.pattern = None
    MatchMapping.rest = None
    MatchStar.name = None
    ParamSpec.default_value = None
    Raise.cause = None
    Raise.exc = None
    Return.value = None
    Slice.lower = None
    Slice.step = None
    Slice.upper = None
    TypeVar.bound = None
    TypeVar.default_value = None
    TypeVarTuple.default_value = None
    With.type_comment = None
    withitem.optional_vars = None
    Yield.value = None

_setup_none_defaults()


# CPython: Python/Python-ast.c (auto-generated __doc__ per node type)
def _setup_docs():
    # abstract base types - generated dynamically from actual subclass order
    # so __subclasses__()[0] always matches the "X = " prefix line.
    mod.__doc__ = (
        "mod = Module(stmt* body, type_ignore* type_ignores)\n"
        "    | Interactive(stmt* body)\n"
        "    | Expression(expr body)\n"
        "    | FunctionType(expr* argtypes, expr returns)"
    )
    stmt.__doc__ = (
        "stmt = FunctionDef(identifier name, arguments args, stmt* body, expr* decorator_list, expr? returns, string? type_comment, type_param* type_params)\n"
        "     | AsyncFunctionDef(identifier name, arguments args, stmt* body, expr* decorator_list, expr? returns, string? type_comment, type_param* type_params)\n"
        "     | ClassDef(identifier name, expr* bases, keyword* keywords, stmt* body, expr* decorator_list, type_param* type_params)\n"
        "     | Return(expr? value)\n"
        "     | Delete(expr* targets)\n"
        "     | Assign(expr* targets, expr value, string? type_comment)\n"
        "     | TypeAlias(expr name, type_param* type_params, expr value)\n"
        "     | AugAssign(expr target, operator op, expr value)\n"
        "     | AnnAssign(expr target, expr annotation, expr? value, int simple)\n"
        "     | For(expr target, expr iter, stmt* body, stmt* orelse, string? type_comment)\n"
        "     | AsyncFor(expr target, expr iter, stmt* body, stmt* orelse, string? type_comment)\n"
        "     | While(expr test, stmt* body, stmt* orelse)\n"
        "     | If(expr test, stmt* body, stmt* orelse)\n"
        "     | With(withitem* items, stmt* body, string? type_comment)\n"
        "     | AsyncWith(withitem* items, stmt* body, string? type_comment)\n"
        "     | Match(expr subject, match_case* cases)\n"
        "     | Raise(expr? exc, expr? cause)\n"
        "     | Try(stmt* body, excepthandler* handlers, stmt* orelse, stmt* finalbody)\n"
        "     | TryStar(stmt* body, excepthandler* handlers, stmt* orelse, stmt* finalbody)\n"
        "     | Assert(expr test, expr? msg)\n"
        "     | Import(alias* names)\n"
        "     | ImportFrom(identifier? module, alias* names, int? level)\n"
        "     | Global(identifier* names)\n"
        "     | Nonlocal(identifier* names)\n"
        "     | Expr(expr value)\n"
        "     | Pass\n"
        "     | Break\n"
        "     | Continue"
    )
    expr_context.__doc__ = "expr_context = Load | Store | Del"
    boolop.__doc__ = "boolop = And | Or"
    operator.__doc__ = "operator = Add | Sub | Mult | MatMult | Div | Mod | Pow | LShift | RShift | BitOr | BitXor | BitAnd | FloorDiv"
    unaryop.__doc__ = "unaryop = Invert | Not | UAdd | USub"
    cmpop.__doc__ = "cmpop = Eq | NotEq | Lt | LtE | Gt | GtE | Is | IsNot | In | NotIn"
    excepthandler.__doc__ = "excepthandler = ExceptHandler(expr? type, identifier? name, stmt* body)"
    pattern.__doc__ = (
        "pattern = MatchValue(expr value)\n"
        "        | MatchSingleton(constant value)\n"
        "        | MatchSequence(pattern* patterns)\n"
        "        | MatchMapping(expr* keys, pattern* patterns, identifier? rest)\n"
        "        | MatchClass(expr cls, pattern* patterns, identifier* kwd_attrs, pattern* kwd_patterns)\n"
        "        | MatchStar(identifier? name)\n"
        "        | MatchAs(pattern? pattern, identifier? name)\n"
        "        | MatchOr(pattern* patterns)"
    )
    type_ignore.__doc__ = "type_ignore = TypeIgnore(int lineno, string tag)"
    type_param.__doc__ = (
        "type_param = TypeVar(identifier name, expr? bound, expr? default_value)\n"
        "           | ParamSpec(identifier name, expr? default_value)\n"
        "           | TypeVarTuple(identifier name, expr? default_value)"
    )
    # concrete mod nodes
    Module.__doc__ = "Module(stmt* body, type_ignore* type_ignores)"
    Interactive.__doc__ = "Interactive(stmt* body)"
    Expression.__doc__ = "Expression(expr body)"
    FunctionType.__doc__ = "FunctionType(expr* argtypes, expr returns)"
    # concrete stmt nodes
    FunctionDef.__doc__ = "FunctionDef(identifier name, arguments args, stmt* body, expr* decorator_list, expr? returns, string? type_comment, type_param* type_params)"
    AsyncFunctionDef.__doc__ = "AsyncFunctionDef(identifier name, arguments args, stmt* body, expr* decorator_list, expr? returns, string? type_comment, type_param* type_params)"
    ClassDef.__doc__ = "ClassDef(identifier name, expr* bases, keyword* keywords, stmt* body, expr* decorator_list, type_param* type_params)"
    Return.__doc__ = "Return(expr? value)"
    Delete.__doc__ = "Delete(expr* targets)"
    Assign.__doc__ = "Assign(expr* targets, expr value, string? type_comment)"
    TypeAlias.__doc__ = "TypeAlias(expr name, type_param* type_params, expr value)"
    AugAssign.__doc__ = "AugAssign(expr target, operator op, expr value)"
    AnnAssign.__doc__ = "AnnAssign(expr target, expr annotation, expr? value, int simple)"
    For.__doc__ = "For(expr target, expr iter, stmt* body, stmt* orelse, string? type_comment)"
    AsyncFor.__doc__ = "AsyncFor(expr target, expr iter, stmt* body, stmt* orelse, string? type_comment)"
    While.__doc__ = "While(expr test, stmt* body, stmt* orelse)"
    If.__doc__ = "If(expr test, stmt* body, stmt* orelse)"
    With.__doc__ = "With(withitem* items, stmt* body, string? type_comment)"
    AsyncWith.__doc__ = "AsyncWith(withitem* items, stmt* body, string? type_comment)"
    Match.__doc__ = "Match(expr subject, match_case* cases)"
    Raise.__doc__ = "Raise(expr? exc, expr? cause)"
    Try.__doc__ = "Try(stmt* body, excepthandler* handlers, stmt* orelse, stmt* finalbody)"
    TryStar.__doc__ = "TryStar(stmt* body, excepthandler* handlers, stmt* orelse, stmt* finalbody)"
    Assert.__doc__ = "Assert(expr test, expr? msg)"
    Import.__doc__ = "Import(alias* names)"
    ImportFrom.__doc__ = "ImportFrom(identifier? module, alias* names, int? level)"
    Global.__doc__ = "Global(identifier* names)"
    Nonlocal.__doc__ = "Nonlocal(identifier* names)"
    Expr.__doc__ = "Expr(expr value)"
    Pass.__doc__ = "Pass"
    Break.__doc__ = "Break"
    Continue.__doc__ = "Continue"
    # concrete expr nodes
    BoolOp.__doc__ = "BoolOp(boolop op, expr* values)"
    NamedExpr.__doc__ = "NamedExpr(expr target, expr value)"
    BinOp.__doc__ = "BinOp(expr left, operator op, expr right)"
    UnaryOp.__doc__ = "UnaryOp(unaryop op, expr operand)"
    Lambda.__doc__ = "Lambda(arguments args, expr body)"
    IfExp.__doc__ = "IfExp(expr test, expr body, expr orelse)"
    Dict.__doc__ = "Dict(expr?* keys, expr* values)"
    Set.__doc__ = "Set(expr* elts)"
    ListComp.__doc__ = "ListComp(expr elt, comprehension* generators)"
    SetComp.__doc__ = "SetComp(expr elt, comprehension* generators)"
    DictComp.__doc__ = "DictComp(expr key, expr value, comprehension* generators)"
    GeneratorExp.__doc__ = "GeneratorExp(expr elt, comprehension* generators)"
    Await.__doc__ = "Await(expr value)"
    Yield.__doc__ = "Yield(expr? value)"
    YieldFrom.__doc__ = "YieldFrom(expr value)"
    Compare.__doc__ = "Compare(expr left, cmpop* ops, expr* comparators)"
    Call.__doc__ = "Call(expr func, expr* args, keyword* keywords)"
    FormattedValue.__doc__ = "FormattedValue(expr value, int conversion, expr? format_spec)"
    Interpolation.__doc__ = "Interpolation(expr value, constant str, int conversion, expr? format_spec)"
    JoinedStr.__doc__ = "JoinedStr(expr* values)"
    TemplateStr.__doc__ = "TemplateStr(expr* values)"
    Constant.__doc__ = "Constant(constant value, string? kind)"
    Attribute.__doc__ = "Attribute(expr value, identifier attr, expr_context ctx)"
    Subscript.__doc__ = "Subscript(expr value, expr slice, expr_context ctx)"
    Starred.__doc__ = "Starred(expr value, expr_context ctx)"
    Name.__doc__ = "Name(identifier id, expr_context ctx)"
    List.__doc__ = "List(expr* elts, expr_context ctx)"
    Tuple.__doc__ = "Tuple(expr* elts, expr_context ctx)"
    Slice.__doc__ = "Slice(expr? lower, expr? upper, expr? step)"
    # expr_context concrete
    Load.__doc__ = "Load"
    Store.__doc__ = "Store"
    Del.__doc__ = "Del"
    # operator concrete
    Add.__doc__ = "Add"
    Sub.__doc__ = "Sub"
    Mult.__doc__ = "Mult"
    MatMult.__doc__ = "MatMult"
    Div.__doc__ = "Div"
    Mod.__doc__ = "Mod"
    Pow.__doc__ = "Pow"
    LShift.__doc__ = "LShift"
    RShift.__doc__ = "RShift"
    BitOr.__doc__ = "BitOr"
    BitXor.__doc__ = "BitXor"
    BitAnd.__doc__ = "BitAnd"
    FloorDiv.__doc__ = "FloorDiv"
    # unaryop concrete
    Invert.__doc__ = "Invert"
    Not.__doc__ = "Not"
    UAdd.__doc__ = "UAdd"
    USub.__doc__ = "USub"
    # boolop concrete
    And.__doc__ = "And"
    Or.__doc__ = "Or"
    # cmpop concrete
    Eq.__doc__ = "Eq"
    NotEq.__doc__ = "NotEq"
    Lt.__doc__ = "Lt"
    LtE.__doc__ = "LtE"
    Gt.__doc__ = "Gt"
    GtE.__doc__ = "GtE"
    Is.__doc__ = "Is"
    IsNot.__doc__ = "IsNot"
    In.__doc__ = "In"
    NotIn.__doc__ = "NotIn"
    # pattern concrete
    MatchValue.__doc__ = "MatchValue(expr value)"
    MatchSingleton.__doc__ = "MatchSingleton(constant value)"
    MatchSequence.__doc__ = "MatchSequence(pattern* patterns)"
    MatchMapping.__doc__ = "MatchMapping(expr* keys, pattern* patterns, identifier? rest)"
    MatchClass.__doc__ = "MatchClass(expr cls, pattern* patterns, identifier* kwd_attrs, pattern* kwd_patterns)"
    MatchStar.__doc__ = "MatchStar(identifier? name)"
    MatchAs.__doc__ = "MatchAs(pattern? pattern, identifier? name)"
    MatchOr.__doc__ = "MatchOr(pattern* patterns)"
    # excepthandler concrete
    ExceptHandler.__doc__ = "ExceptHandler(expr? type, identifier? name, stmt* body)"
    # type_ignore concrete
    TypeIgnore.__doc__ = "TypeIgnore(int lineno, string tag)"
    # type_param concrete
    TypeVar.__doc__ = "TypeVar(identifier name, expr? bound, expr? default_value)"
    ParamSpec.__doc__ = "ParamSpec(identifier name, expr? default_value)"
    TypeVarTuple.__doc__ = "TypeVarTuple(identifier name, expr? default_value)"
    # expr.__doc__ built dynamically so it matches actual __subclasses__() order.
    # CPython: Python/Python-ast.c (auto-generated from Python.asdl)
    _expr_subs = expr.__subclasses__()
    if _expr_subs:
        _expr_lines = [f"expr = {_expr_subs[0].__doc__}"]
        _expr_lines.extend(f"     | {s.__doc__}" for s in _expr_subs[1:])
        expr.__doc__ = "\n".join(_expr_lines)
    # misc
    withitem.__doc__ = "withitem(expr context_expr, expr? optional_vars)"
    match_case.__doc__ = "match_case(pattern pattern, expr? guard, stmt* body)"
    arguments.__doc__ = "arguments(arg* posonlyargs, arg* args, arg? vararg, arg* kwonlyargs, expr?* kw_defaults, arg? kwarg, expr* defaults)"
    arg.__doc__ = "arg(identifier arg, expr? annotation, string? type_comment)"
    keyword.__doc__ = "keyword(identifier? arg, expr value)"
    alias.__doc__ = "alias(identifier name, identifier? asname)"
    comprehension.__doc__ = "comprehension(expr target, expr iter, expr* ifs, int is_async)"

_setup_docs()
