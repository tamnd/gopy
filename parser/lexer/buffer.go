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
// CPython: Parser/lexer/buffer.c:50 _PyLexer_tok_reserve_buf
func (s *State) reserveBuf(size int) {
	have := s.end - s.inp
	if have >= size {
		return
	}
	need := s.inp + size
	if grow := s.inp + (s.inp >> 1); grow > need {
		need = grow
	}
	if cap(s.buf) >= need {
		s.buf = s.buf[:need]
	} else {
		nb := make([]byte, need)
		copy(nb, s.buf[:s.inp])
		s.buf = nb
	}
	s.end = need
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
