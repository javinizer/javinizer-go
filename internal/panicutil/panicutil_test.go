package panicutil

import (
	"errors"
	"strings"
	"testing"
)

func TestFormatRecover_NilInput(t *testing.T) {
	err := FormatRecover(nil)
	if err != nil {
		t.Errorf("expected nil error for nil input, got %v", err)
	}
}

func TestFormatRecover_StringPanic(t *testing.T) {
	err := FormatRecover("something broke")
	if err == nil {
		t.Fatal("expected error for non-nil input")
	}
	if !strings.Contains(err.Error(), "panic: something broke") {
		t.Errorf("error should contain 'panic: something broke', got %q", err.Error())
	}
}

func TestFormatRecover_ErrorPanic(t *testing.T) {
	original := errors.New("base error")
	err := FormatRecover(original)
	if err == nil {
		t.Fatal("expected error for non-nil input")
	}
	if !strings.Contains(err.Error(), "panic: base error") {
		t.Errorf("error should contain 'panic: base error', got %q", err.Error())
	}
}

func TestFormatRecover_IntPanic(t *testing.T) {
	err := FormatRecover(42)
	if err == nil {
		t.Fatal("expected error for non-nil input")
	}
	if !strings.Contains(err.Error(), "panic: 42") {
		t.Errorf("error should contain 'panic: 42', got %q", err.Error())
	}
}

func TestFormatRecoverWithStack_NilInput(t *testing.T) {
	err := FormatRecoverWithStack(nil)
	if err != nil {
		t.Errorf("expected nil error for nil input, got %v", err)
	}
}

func TestFormatRecoverWithStack_IncludesStack(t *testing.T) {
	err := FormatRecoverWithStack("boom")
	if err == nil {
		t.Fatal("expected error for non-nil input")
	}
	msg := err.Error()
	if !strings.Contains(msg, "panic: boom") {
		t.Errorf("error should contain 'panic: boom', got %q", msg)
	}
	if !strings.Contains(msg, "goroutine") {
		t.Errorf("error should contain stack trace (goroutine), got %q", msg)
	}
}

func TestHandleRecover_NilInput(t *testing.T) {
	err := HandleRecover(nil)
	if err != nil {
		t.Errorf("expected nil error for nil input, got %v", err)
	}
}

func TestHandleRecover_ReturnsFormattedError(t *testing.T) {
	err := HandleRecover("test panic")
	if err == nil {
		t.Fatal("expected error for non-nil input")
	}
	if !strings.Contains(err.Error(), "panic: test panic") {
		t.Errorf("error should contain 'panic: test panic', got %q", err.Error())
	}
}

func TestHandleRecoverWithStack_NilInput(t *testing.T) {
	err := HandleRecoverWithStack(nil)
	if err != nil {
		t.Errorf("expected nil error for nil input, got %v", err)
	}
}

func TestHandleRecoverWithStack_SanitizedError(t *testing.T) {
	err := HandleRecoverWithStack("boom")
	if err == nil {
		t.Fatal("expected error for non-nil input")
	}
	msg := err.Error()
	if !strings.Contains(msg, "panic: boom") {
		t.Errorf("error should contain 'panic: boom', got %q", msg)
	}
	// The returned error is sanitized: the stack trace is logged for debugging
	// but must NOT be leaked through the error to CLI/API users.
	if strings.Contains(msg, "goroutine") {
		t.Errorf("error should not leak stack trace (goroutine), got %q", msg)
	}
}
