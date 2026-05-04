package ast

import "fmt"

// Warning is a non-fatal preprocess diagnostic. PEP 765 emits one of
// these for each `return`/`break`/`continue` that escapes a `finally`
// block. CPython spells them via _PyErr_EmitSyntaxWarning.
//
// CPython: Python/ast_preprocess.c control_flow_in_finally_warning
type Warning struct {
	Msg      string
	Filename string
	Pos      Pos
}

func (w Warning) Error() string {
	return fmt.Sprintf("%s:%d:%d: SyntaxWarning: %s",
		w.Filename, w.Pos.Lineno, w.Pos.ColOffset+1, w.Msg)
}

// PreprocessOptions controls which preprocess passes run. Mirrors the
// _PyASTPreprocessState flag bag.
//
// CPython: Python/ast_preprocess.c _PyASTPreprocessState
type PreprocessOptions struct {
	Filename       string
	OptimizeLevel  int  // -1 default, 0 = no, 1 = -O, 2 = -OO.
	EnableWarnings bool // false under -W ignore
}

// Preprocess walks mod and emits PEP 765 control-flow-in-finally
// warnings. Constant folding (the bulk of ast_preprocess.c) is a
// separate pass that lands as the codegen visitor list grows.
//
// Returns the slice of warnings; the boolean is `true` if any
// SyntaxError-class issue was produced (none yet at this layer).
//
// CPython: Python/ast_preprocess.c _PyAST_Preprocess
func Preprocess(mod Mod, opt PreprocessOptions) []Warning {
	p := &preprocessor{opt: opt}
	switch m := mod.(type) {
	case *Module:
		p.walkBody(m.Body)
	case *Interactive:
		p.walkBody(m.Body)
	case *Expression, *FunctionType:
		// Expression / FunctionType have no statements; PEP 765 does
		// not apply.
	}
	return p.warnings
}

type cfFrame struct {
	inFinally bool
	inFuncDef bool
	inLoop    bool
}

type preprocessor struct {
	opt      PreprocessOptions
	stack    []cfFrame
	warnings []Warning
}

func (p *preprocessor) push(f cfFrame) { p.stack = append(p.stack, f) }
func (p *preprocessor) pop()           { p.stack = p.stack[:len(p.stack)-1] }

// walkBody walks a Seq[Stmt] in document order.
func (p *preprocessor) walkBody(body Seq[Stmt]) {
	for i := 0; i < body.Len(); i++ {
		p.walkStmt(body.Get(i))
	}
}

// walkStmt dispatches per statement kind. Only kinds that affect the
// PEP 765 cf-context or carry a body need explicit handling; all other
// kinds are leaves for this pass.
//
// CPython: astfold_stmt
func (p *preprocessor) walkStmt(s Stmt) {
	switch n := s.(type) {
	case *FunctionDef:
		p.walkFunc(n.Body)
	case *AsyncFunctionDef:
		p.walkFunc(n.Body)
	case *ClassDef:
		p.walkBody(n.Body)
	case *For:
		p.walkLoop(n.Body, n.Orelse)
	case *AsyncFor:
		p.walkLoop(n.Body, n.Orelse)
	case *While:
		p.walkLoop(n.Body, n.Orelse)
	case *If:
		p.walkBody(n.Body)
		p.walkBody(n.Orelse)
	case *With:
		p.walkBody(n.Body)
	case *AsyncWith:
		p.walkBody(n.Body)
	case *Try:
		p.walkTry(n.Body, n.Handlers, n.Orelse, n.Finalbody)
	case *TryStar:
		p.walkTry(n.Body, n.Handlers, n.Orelse, n.Finalbody)
	case *Match:
		for i := 0; i < n.Cases.Len(); i++ {
			p.walkBody(n.Cases.Get(i).Body)
		}
	case *Return:
		p.checkFinallyExit("return", n.Position())
	case *Break:
		p.checkLoopExit("break", n.Position())
	case *Continue:
		p.checkLoopExit("continue", n.Position())
	}
}

func (p *preprocessor) walkFunc(b Seq[Stmt]) {
	p.push(cfFrame{inFuncDef: true})
	p.walkBody(b)
	p.pop()
}

func (p *preprocessor) walkLoop(b, orelse Seq[Stmt]) {
	p.push(cfFrame{inLoop: true})
	p.walkBody(b)
	p.pop()
	p.walkBody(orelse)
}

func (p *preprocessor) walkTry(b Seq[Stmt], hs Seq[Excepthandler], orelse, finalbody Seq[Stmt]) {
	p.walkBody(b)
	for i := 0; i < hs.Len(); i++ {
		if h, ok := hs.Get(i).(*ExceptHandler); ok {
			p.walkBody(h.Body)
		}
	}
	p.walkBody(orelse)
	p.push(cfFrame{inFinally: true})
	p.walkBody(finalbody)
	p.pop()
}

// checkFinallyExit mirrors before_return: a `return` is flagged if any
// enclosing finally block is active *before* an enclosing funcdef.
func (p *preprocessor) checkFinallyExit(kw string, pos Pos) {
	if !p.opt.EnableWarnings {
		return
	}
	for i := len(p.stack) - 1; i >= 0; i-- {
		f := p.stack[i]
		if f.inFuncDef {
			return
		}
		if f.inFinally {
			p.emit(kw, pos)
			return
		}
	}
}

// checkLoopExit mirrors before_loop_exit: a `break`/`continue` is
// flagged if any enclosing finally is active before an enclosing loop.
func (p *preprocessor) checkLoopExit(kw string, pos Pos) {
	if !p.opt.EnableWarnings {
		return
	}
	for i := len(p.stack) - 1; i >= 0; i-- {
		f := p.stack[i]
		if f.inLoop {
			return
		}
		if f.inFinally {
			p.emit(kw, pos)
			return
		}
	}
}

func (p *preprocessor) emit(kw string, pos Pos) {
	p.warnings = append(p.warnings, Warning{
		Msg:      fmt.Sprintf("'%s' in a 'finally' block", kw),
		Filename: p.opt.Filename,
		Pos:      pos,
	})
}
