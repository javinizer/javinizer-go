// Package panicutil provides shared panic recovery utilities.
//
// Instead of scattering inline recover() blocks with ad-hoc logging and error
// formatting across the codebase, callers use HandleRecover or HandleRecoverWithStack
// to centralise the panic→error conversion and logging.
//
// Note: The logging package itself cannot use panicutil (circular import), so
// it retains its inline recover() pattern. All other packages should use panicutil.
//
// Typical usage:
//
//	defer func() {
//	    if r := recover(); r != nil {
//	        err = panicutil.HandleRecover(r)
//	    }
//	}()
package panicutil

import (
	"fmt"
	"runtime/debug"

	"github.com/javinizer/javinizer-go/internal/logging"
)

// FormatRecover converts a recovered panic value into a formatted error.
// Returns nil when recovered is nil (no panic). Unlike HandleRecover, this
// function does not log — callers that need custom context in their log
// messages should use FormatRecover and handle logging themselves.
func FormatRecover(recovered interface{}) error {
	if recovered == nil {
		return nil
	}
	return fmt.Errorf("panic: %v", recovered)
}

// HandleRecover converts a recovered panic value into a formatted error and
// logs it at error level. Returns nil when recovered is nil (no panic).
//
// Contract: this helper converts a recovered panic into an error and logs it;
// it does NOT re-panic. Do not use it where crash/re-panic semantics are
// required — callers that need to abort the process should re-panic explicitly
// after handling.
//
// Use this in defer/recover blocks to centralise panic handling:
//
//	defer func() {
//	    if r := recover(); r != nil {
//	        err = panicutil.HandleRecover(r)
//	    }
//	}()
func HandleRecover(recovered interface{}) error {
	if recovered == nil {
		return nil
	}
	err := FormatRecover(recovered)
	logging.Errorf("%v", err)
	return err
}

// HandleRecoverWithStack is like HandleRecover but logs the goroutine stack
// trace for debugging. The returned error is sanitized (it does NOT contain
// the stack trace) so goroutine internals are not leaked to CLI/API users.
//
// Contract: like HandleRecover, this converts a recovered panic into an error
// and logs it; it does NOT re-panic. Do not use it where crash/re-panic
// semantics are required. The stack trace is logged only — use
// FormatRecoverWithStack when you need the stack-bearing string for logging.
func HandleRecoverWithStack(recovered interface{}) error {
	if recovered == nil {
		return nil
	}
	// Log the stack-bearing message for debugging, but return the sanitized
	// (stack-free) error so debug.Stack output is not surfaced to callers.
	logging.Errorf("%v", FormatRecoverWithStack(recovered))
	return FormatRecover(recovered)
}

// FormatRecoverWithStack is like FormatRecover but includes the goroutine
// stack trace in the error message. Does not log.
func FormatRecoverWithStack(recovered interface{}) error {
	if recovered == nil {
		return nil
	}
	return fmt.Errorf("panic: %v\n%s", recovered, debug.Stack())
}
