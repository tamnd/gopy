// CPython: Parser/lexer/buffer.c, Parser/lexer/buffer.h
//
// The C source juggles raw char* pointers into the source buffer, so
// growing it requires a remember/restore dance that walks the f-string
// mode stack and rebases every saved pointer. gopy stores offsets
// instead of pointers, so the dance disappears: we just grow the slice
// and the offsets stay valid.

package lexer

// reserveBuf ensures buf has room for size more bytes past inp.
//
// Mirrors CPython's growth policy: newsize = oldsize + max(size,
// oldsize/2). The pointer remember/restore dance disappears because
// gopy stores offsets, so growing the slice does not invalidate
// anything saved on the f-string mode stack.
//
// CPython: Parser/lexer/buffer.c:50 _PyLexer_tok_reserve_buf
func (s *State) reserveBuf(size int) {
	oldsize := s.inp
	grow := oldsize >> 1
	if size > grow {
		grow = size
	}
	newsize := oldsize + grow
	if newsize <= s.end {
		return
	}
	s.rememberFStringBuffers()
	if cap(s.buf) >= newsize {
		s.buf = s.buf[:newsize]
	} else {
		nb := make([]byte, newsize)
		copy(nb, s.buf[:s.inp])
		s.buf = nb
	}
	s.end = newsize
	s.restoreFStringBuffers()
}

// rememberFStringBuffers is a no-op in gopy since the f-string mode
// stack already uses offsets. Kept as a documentation anchor.
//
// CPython: Parser/lexer/buffer.c:9 _PyLexer_remember_fstring_buffers
func (s *State) rememberFStringBuffers() {}

// restoreFStringBuffers is the matching no-op for the restore side.
//
// CPython: Parser/lexer/buffer.c:23 _PyLexer_restore_fstring_buffers
func (s *State) restoreFStringBuffers() {}
