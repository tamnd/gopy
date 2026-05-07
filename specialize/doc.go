// Package specialize ports cpython/Python/specialize.c. PEP 659
// adaptive specialization rewrites adaptive opcodes (LOAD_ATTR,
// BINARY_OP, CALL, ...) into specialized variants on warmup, and
// falls back to the adaptive parent on shape mismatch. The 16-bit
// backoff counter in every adaptive instruction's first cache slot
// drives the rewrite cadence.
//
// v0.11 lays down the foundation: backoff counter helpers, inline
// cache struct layouts, the deopt table, and a Quicken pass that
// stamps the warmup counter into every adaptive cache slot. The
// per-family specializer entry points (LoadAttr, BinaryOp, Call,
// ...) follow on top.
//
// CPython: Python/specialize.c
package specialize
