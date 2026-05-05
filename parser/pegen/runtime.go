// CPython: Parser/pegen.c runtime helpers (memo init, error
// reporting, debug logging). The token buffer fill loop lives in
// fill.go alongside the lookahead primitives. This file holds the
// rest: parser construction surface, the recursion-depth guard,
// and the error-indicator plumbing.

package pegen

// MaxRecursionDepth bounds left-recursive rule depth. The
// generated parser bumps p.level on each rule entry and
// decrements on exit; trip the guard with StackOverflow().
//
// CPython: Parser/pegen.h:60 _PyOS_RecursionLimit
const MaxRecursionDepth = 6000

// EnterFrame is called at the head of every generated rule. It
// returns false when the depth guard trips, in which case the
// rule must return nil and stop walking.
//
// CPython: Parser/pegen.c:902 _PyPegen_check_recursion_limit
func (p *Parser) EnterFrame() bool {
	p.level++
	return p.level <= MaxRecursionDepth
}

// LeaveFrame matches EnterFrame; safe to call even when the guard
// tripped because the depth counter is symmetric.
func (p *Parser) LeaveFrame() {
	p.level--
}

// SetCallInvalid flips the second-pass flag the generated parser
// reads when invalid_ rules should fire.
//
// CPython: Parser/pegen.c:946 _PyPegen_run_parser
func (p *Parser) SetCallInvalid(v bool) { p.callInvalid = v }

// CallInvalid reports the second-pass flag.
func (p *Parser) CallInvalid() bool { return p.callInvalid }

// Debug enables verbose rule-trace output. The generated parser
// reads this in the dispatch loop; gopy uses it identically.
//
// CPython: Parser/pegen.c:973 _PyPegen_set_debug
func (p *Parser) Debug(v bool) { p.debug = v }

// FeatureVersion pins the from __future__ minor version the
// parser is targeting. Defaults to current; rules that gate on
// PEP-introduced syntax read this field.
//
// CPython: Parser/pegen.h:88 feature_version
func (p *Parser) FeatureVersion() int { return p.featureVersion }

// SetFeatureVersion pins the from __future__ minor version.
func (p *Parser) SetFeatureVersion(v int) { p.featureVersion = v }

// Flags exposes the parse-time flag bitset.
//
// CPython: Parser/pegen.h:84 flags
func (p *Parser) Flags() int { return p.flags }
