package initconfig

import "testing"

func TestStatusOk(t *testing.T) {
	s := StatusOk()
	if s.Type != StatusOK || s.IsException() || s.IsError() || s.IsExit() {
		t.Fatalf("StatusOk: %+v", s)
	}
}

func TestStatusErr(t *testing.T) {
	s := StatusErr("boom")
	if !s.IsError() || !s.IsException() || s.IsExit() {
		t.Fatalf("StatusErr classification: %+v", s)
	}
	if s.ErrMsg != "boom" {
		t.Fatalf("ErrMsg = %q", s.ErrMsg)
	}
}

func TestStatusExitCode(t *testing.T) {
	s := StatusExitCode(2)
	if !s.IsExit() || !s.IsException() || s.IsError() {
		t.Fatalf("StatusExitCode classification: %+v", s)
	}
	if s.ExitCode != 2 {
		t.Fatalf("ExitCode = %d", s.ExitCode)
	}
}

func TestStatusNoMemory(t *testing.T) {
	s := StatusNoMemory()
	if !s.IsError() {
		t.Fatalf("StatusNoMemory should be error, got %+v", s)
	}
	if s.ErrMsg == "" {
		t.Fatalf("StatusNoMemory should carry a message")
	}
}
