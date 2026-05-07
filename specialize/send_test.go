package specialize

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

func sendBuf() []byte {
	buf := make([]byte, 2*(1+CacheCount(compile.SEND)))
	buf[0] = byte(compile.SEND)
	return buf
}

func TestSendGenerator(t *testing.T) {
	buf := sendBuf()
	Send(objects.NewGenerator("g"), buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.SEND_GEN {
		t.Fatalf("opcode: got %s want SEND_GEN", got.Name())
	}
}

func TestSendCoroutine(t *testing.T) {
	buf := sendBuf()
	Send(objects.NewCoroutine("c"), buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.SEND_GEN {
		t.Fatalf("opcode: got %s want SEND_GEN", got.Name())
	}
}

func TestSendUnspecializeOther(t *testing.T) {
	buf := sendBuf()
	Send(objects.NewList(nil), buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.SEND {
		t.Fatalf("opcode: got %s want SEND", got.Name())
	}
}
