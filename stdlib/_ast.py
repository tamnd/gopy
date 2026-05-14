# Minimal stub for the _ast C extension. Provides the AST node classes
# that annotationlib.py and ast.py need. Real CPython gets these from a
# C extension built from Python.asdl; gopy has no _ast C module yet so
# we provide pure-Python equivalents.
#
# Each node class stores its constructor kwargs as instance attributes.
# The ast.unparse() writer only reads those fields, so the shape is
# enough for annotationlib's Format.STRING path.

PyCF_ONLY_AST = 0x0400
PyCF_TYPE_COMMENTS = 0x1000
PyCF_ALLOW_TOP_LEVEL_AWAIT = 0x0200


class AST:
    _fields = ()
    _attributes = ('lineno', 'col_offset', 'end_lineno', 'end_col_offset')

    def __init__(self, *args, **kwargs):
        fields = self._fields
        for i, v in enumerate(args):
            if i < len(fields):
                setattr(self, fields[i], v)
        for k, v in kwargs.items():
            setattr(self, k, v)

    def __repr__(self):
        parts = []
        for f in self._fields:
            if hasattr(self, f):
                parts.append(f'{f}={getattr(self, f)!r}')
        return f'{type(self).__name__}({", ".join(parts)})'


# -- expression context --

class expr_context(AST): pass
class Load(expr_context): _fields = ()
class Store(expr_context): _fields = ()
class Del(expr_context): _fields = ()

# -- operators --

class operator(AST): pass
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

class unaryop(AST): pass
class Invert(unaryop): _fields = ()
class Not(unaryop): _fields = ()
class UAdd(unaryop): _fields = ()
class USub(unaryop): _fields = ()

# -- comparison operators --

class cmpop(AST): pass
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

class boolop(AST): pass
class And(boolop): _fields = ()
class Or(boolop): _fields = ()

# -- expression nodes --

class expr(AST): pass

class Name(expr):
    _fields = ('id', 'ctx')
    def __init__(self, id=None, ctx=None, **kw):
        self.id = id
        self.ctx = ctx or Load()
        for k, v in kw.items():
            setattr(self, k, v)

class Constant(expr):
    _fields = ('value', 'kind')
    def __init__(self, value=None, kind=None, **kw):
        self.value = value
        self.kind = kind
        for k, v in kw.items():
            setattr(self, k, v)

class Attribute(expr):
    _fields = ('value', 'attr', 'ctx')
    def __init__(self, value=None, attr=None, ctx=None, **kw):
        self.value = value
        self.attr = attr
        self.ctx = ctx or Load()
        for k, v in kw.items():
            setattr(self, k, v)

class Subscript(expr):
    _fields = ('value', 'slice', 'ctx')
    def __init__(self, value=None, slice=None, ctx=None, **kw):
        self.value = value
        self.slice = slice
        self.ctx = ctx or Load()
        for k, v in kw.items():
            setattr(self, k, v)

class Starred(expr):
    _fields = ('value', 'ctx')
    def __init__(self, value=None, ctx=None, **kw):
        self.value = value
        self.ctx = ctx or Load()
        for k, v in kw.items():
            setattr(self, k, v)

class List(expr):
    _fields = ('elts', 'ctx')
    def __init__(self, elts=None, ctx=None, **kw):
        self.elts = elts if elts is not None else []
        self.ctx = ctx or Load()
        for k, v in kw.items():
            setattr(self, k, v)

class Tuple(expr):
    _fields = ('elts', 'ctx')
    def __init__(self, elts=None, ctx=None, **kw):
        self.elts = elts if elts is not None else []
        self.ctx = ctx or Load()
        for k, v in kw.items():
            setattr(self, k, v)

class Set(expr):
    _fields = ('elts',)
    def __init__(self, elts=None, **kw):
        self.elts = elts if elts is not None else []
        for k, v in kw.items():
            setattr(self, k, v)

class Dict(expr):
    _fields = ('keys', 'values')
    def __init__(self, keys=None, values=None, **kw):
        self.keys = keys if keys is not None else []
        self.values = values if values is not None else []
        for k, v in kw.items():
            setattr(self, k, v)

class BinOp(expr):
    _fields = ('left', 'op', 'right')
    def __init__(self, left=None, op=None, right=None, **kw):
        self.left = left
        self.op = op
        self.right = right
        for k, v in kw.items():
            setattr(self, k, v)

class UnaryOp(expr):
    _fields = ('op', 'operand')
    def __init__(self, op=None, operand=None, **kw):
        self.op = op
        self.operand = operand
        for k, v in kw.items():
            setattr(self, k, v)

class BoolOp(expr):
    _fields = ('op', 'values')
    def __init__(self, op=None, values=None, **kw):
        self.op = op
        self.values = values if values is not None else []
        for k, v in kw.items():
            setattr(self, k, v)

class Compare(expr):
    _fields = ('left', 'ops', 'comparators')
    def __init__(self, left=None, ops=None, comparators=None, **kw):
        self.left = left
        self.ops = ops if ops is not None else []
        self.comparators = comparators if comparators is not None else []
        for k, v in kw.items():
            setattr(self, k, v)

class Call(expr):
    _fields = ('func', 'args', 'keywords')
    def __init__(self, func=None, args=None, keywords=None, **kw):
        self.func = func
        self.args = args if args is not None else []
        self.keywords = keywords if keywords is not None else []
        for k, v in kw.items():
            setattr(self, k, v)

class IfExp(expr):
    _fields = ('test', 'body', 'orelse')
    def __init__(self, test=None, body=None, orelse=None, **kw):
        self.test = test
        self.body = body
        self.orelse = orelse
        for k, v in kw.items():
            setattr(self, k, v)

class Slice(expr):
    _fields = ('lower', 'upper', 'step')
    def __init__(self, lower=None, upper=None, step=None, **kw):
        self.lower = lower
        self.upper = upper
        self.step = step
        for k, v in kw.items():
            setattr(self, k, v)

class Lambda(expr):
    _fields = ('args', 'body')
    def __init__(self, args=None, body=None, **kw):
        self.args = args
        self.body = body
        for k, v in kw.items():
            setattr(self, k, v)

class NamedExpr(expr):
    _fields = ('target', 'value')
    def __init__(self, target=None, value=None, **kw):
        self.target = target
        self.value = value
        for k, v in kw.items():
            setattr(self, k, v)

# -- misc nodes --

class keyword(AST):
    _fields = ('arg', 'value')
    def __init__(self, arg=None, value=None, **kw):
        self.arg = arg
        self.value = value
        for k, v in kw.items():
            setattr(self, k, v)

class arg(AST):
    _fields = ('arg', 'annotation', 'type_comment')
    def __init__(self, arg=None, annotation=None, type_comment=None, **kw):
        self.arg = arg
        self.annotation = annotation
        self.type_comment = type_comment
        for k, v in kw.items():
            setattr(self, k, v)

class arguments(AST):
    _fields = ('posonlyargs', 'args', 'vararg', 'kwonlyargs',
               'kw_defaults', 'kwarg', 'defaults')
    def __init__(self, posonlyargs=None, args=None, vararg=None,
                 kwonlyargs=None, kw_defaults=None, kwarg=None,
                 defaults=None, **kw):
        self.posonlyargs = posonlyargs or []
        self.args = args or []
        self.vararg = vararg
        self.kwonlyargs = kwonlyargs or []
        self.kw_defaults = kw_defaults or []
        self.kwarg = kwarg
        self.defaults = defaults or []
        for k, v in kw.items():
            setattr(self, k, v)

# -- top-level grammar groups --

class mod(AST): pass
class stmt(AST): pass

class Module(mod):
    _fields = ('body', 'type_ignores')
    def __init__(self, body=None, type_ignores=None, **kw):
        self.body = body or []
        self.type_ignores = type_ignores or []
        for k, v in kw.items():
            setattr(self, k, v)

class Expression(mod):
    _fields = ('body',)
    def __init__(self, body=None, **kw):
        self.body = body
        for k, v in kw.items():
            setattr(self, k, v)

class Interactive(mod):
    _fields = ('body',)
    def __init__(self, body=None, **kw):
        self.body = body or []
        for k, v in kw.items():
            setattr(self, k, v)

class FunctionType(mod):
    _fields = ('argtypes', 'returns')
    def __init__(self, argtypes=None, returns=None, **kw):
        self.argtypes = argtypes or []
        self.returns = returns
        for k, v in kw.items():
            setattr(self, k, v)

class FunctionDef(stmt):
    _fields = ('name', 'args', 'body', 'decorator_list', 'returns',
               'type_comment', 'type_params')
    def __init__(self, **kw):
        for k, v in kw.items():
            setattr(self, k, v)

class AsyncFunctionDef(stmt):
    _fields = ('name', 'args', 'body', 'decorator_list', 'returns',
               'type_comment', 'type_params')
    def __init__(self, **kw):
        for k, v in kw.items():
            setattr(self, k, v)

class ClassDef(stmt):
    _fields = ('name', 'bases', 'keywords', 'body', 'decorator_list',
               'type_params')
    def __init__(self, **kw):
        for k, v in kw.items():
            setattr(self, k, v)

class Return(stmt):
    _fields = ('value',)
    def __init__(self, value=None, **kw):
        self.value = value
        for k, v in kw.items():
            setattr(self, k, v)

class Assign(stmt):
    _fields = ('targets', 'value', 'type_comment')
    def __init__(self, **kw):
        for k, v in kw.items():
            setattr(self, k, v)

class AnnAssign(stmt):
    _fields = ('target', 'annotation', 'value', 'simple')
    def __init__(self, **kw):
        for k, v in kw.items():
            setattr(self, k, v)

class Expr(stmt):
    _fields = ('value',)
    def __init__(self, value=None, **kw):
        self.value = value
        for k, v in kw.items():
            setattr(self, k, v)

class Pass(stmt): _fields = ()
class Break(stmt): _fields = ()
class Continue(stmt): _fields = ()

class Raise(stmt):
    _fields = ('exc', 'cause')
    def __init__(self, exc=None, cause=None, **kw):
        self.exc = exc
        self.cause = cause
        for k, v in kw.items():
            setattr(self, k, v)

class If(stmt):
    _fields = ('test', 'body', 'orelse')
    def __init__(self, **kw):
        for k, v in kw.items():
            setattr(self, k, v)

class For(stmt):
    _fields = ('target', 'iter', 'body', 'orelse', 'type_comment')
    def __init__(self, **kw):
        for k, v in kw.items():
            setattr(self, k, v)

class While(stmt):
    _fields = ('test', 'body', 'orelse')
    def __init__(self, **kw):
        for k, v in kw.items():
            setattr(self, k, v)

class With(stmt):
    _fields = ('items', 'body', 'type_comment')
    def __init__(self, **kw):
        for k, v in kw.items():
            setattr(self, k, v)

class withitem(AST):
    _fields = ('context_expr', 'optional_vars')
    def __init__(self, **kw):
        for k, v in kw.items():
            setattr(self, k, v)

class Import(stmt):
    _fields = ('names',)
    def __init__(self, **kw):
        for k, v in kw.items():
            setattr(self, k, v)

class ImportFrom(stmt):
    _fields = ('module', 'names', 'level')
    def __init__(self, **kw):
        for k, v in kw.items():
            setattr(self, k, v)

class alias(AST):
    _fields = ('name', 'asname')
    def __init__(self, name=None, asname=None, **kw):
        self.name = name
        self.asname = asname
        for k, v in kw.items():
            setattr(self, k, v)

class Global(stmt):
    _fields = ('names',)
    def __init__(self, **kw):
        for k, v in kw.items():
            setattr(self, k, v)

class Nonlocal(stmt):
    _fields = ('names',)
    def __init__(self, **kw):
        for k, v in kw.items():
            setattr(self, k, v)

class Delete(stmt):
    _fields = ('targets',)
    def __init__(self, **kw):
        for k, v in kw.items():
            setattr(self, k, v)

class Assert(stmt):
    _fields = ('test', 'msg')
    def __init__(self, **kw):
        for k, v in kw.items():
            setattr(self, k, v)

class Try(stmt):
    _fields = ('body', 'handlers', 'orelse', 'finalbody')
    def __init__(self, **kw):
        for k, v in kw.items():
            setattr(self, k, v)

class TryStar(stmt):
    _fields = ('body', 'handlers', 'orelse', 'finalbody')
    def __init__(self, **kw):
        for k, v in kw.items():
            setattr(self, k, v)

class ExceptHandler(AST):
    _fields = ('type', 'name', 'body')
    def __init__(self, **kw):
        for k, v in kw.items():
            setattr(self, k, v)

class TypeIgnore(AST):
    _fields = ('lineno', 'tag')
    def __init__(self, **kw):
        for k, v in kw.items():
            setattr(self, k, v)

class TypeVar(AST):
    _fields = ('name', 'bound', 'default_value')
    def __init__(self, **kw):
        for k, v in kw.items():
            setattr(self, k, v)

class TypeVarTuple(AST):
    _fields = ('name', 'default_value')
    def __init__(self, **kw):
        for k, v in kw.items():
            setattr(self, k, v)

class ParamSpec(AST):
    _fields = ('name', 'default_value')
    def __init__(self, **kw):
        for k, v in kw.items():
            setattr(self, k, v)

class TypeAlias(stmt):
    _fields = ('name', 'type_params', 'value')
    def __init__(self, **kw):
        for k, v in kw.items():
            setattr(self, k, v)

# Template string nodes (PEP 750)
class TemplateStr(expr):
    _fields = ('values',)
    def __init__(self, values=None, **kw):
        self.values = values or []
        for k, v in kw.items():
            setattr(self, k, v)

class Interpolation(expr):
    _fields = ('value', 'expression', 'conversion', 'format_spec')
    def __init__(self, value=None, expression=None, conversion=-1,
                 format_spec=None, **kw):
        self.value = value
        self.expression = expression
        self.conversion = conversion
        self.format_spec = format_spec
        for k, v in kw.items():
            setattr(self, k, v)

# JoinedStr (f-string)
class JoinedStr(expr):
    _fields = ('values',)
    def __init__(self, values=None, **kw):
        self.values = values or []
        for k, v in kw.items():
            setattr(self, k, v)

class FormattedValue(expr):
    _fields = ('value', 'conversion', 'format_spec')
    def __init__(self, value=None, conversion=-1, format_spec=None, **kw):
        self.value = value
        self.conversion = conversion
        self.format_spec = format_spec
        for k, v in kw.items():
            setattr(self, k, v)

# Comprehension
class comprehension(AST):
    _fields = ('target', 'iter', 'ifs', 'is_async')
    def __init__(self, **kw):
        for k, v in kw.items():
            setattr(self, k, v)

class ListComp(expr):
    _fields = ('elt', 'generators')
    def __init__(self, **kw):
        for k, v in kw.items():
            setattr(self, k, v)

class SetComp(expr):
    _fields = ('elt', 'generators')
    def __init__(self, **kw):
        for k, v in kw.items():
            setattr(self, k, v)

class GeneratorExp(expr):
    _fields = ('elt', 'generators')
    def __init__(self, **kw):
        for k, v in kw.items():
            setattr(self, k, v)

class DictComp(expr):
    _fields = ('key', 'value', 'generators')
    def __init__(self, **kw):
        for k, v in kw.items():
            setattr(self, k, v)

class Await(expr):
    _fields = ('value',)
    def __init__(self, value=None, **kw):
        self.value = value
        for k, v in kw.items():
            setattr(self, k, v)

class Yield(expr):
    _fields = ('value',)
    def __init__(self, value=None, **kw):
        self.value = value
        for k, v in kw.items():
            setattr(self, k, v)

class YieldFrom(expr):
    _fields = ('value',)
    def __init__(self, value=None, **kw):
        self.value = value
        for k, v in kw.items():
            setattr(self, k, v)
