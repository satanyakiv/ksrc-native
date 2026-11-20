// Package errs defines the coded errors that form the tool's error contract.
//
// Every user-facing failure is printed as
//
//	E_CODE: message. Try: hint
//
// The code is stable, so agents and scripts can match on it.
package errs

import (
	"errors"
	"strings"
)

// Stable error codes.
const (
	NoKonan    = "E_NO_KONAN"    // no Kotlin/Native distribution was found
	NoSources  = "E_NO_SOURCES"  // the coordinate has no searchable sources
	NoRipgrep  = "E_NO_RG"       // the rg binary is missing
	NoXcode    = "E_NO_XCODE"    // Xcode or its command-line tools are missing
	NoFile     = "E_NO_FILE"     // a file-id points at a file that does not exist
	BadCoord   = "E_BAD_COORD"   // a module coordinate cannot be parsed or used
	BadFileID  = "E_BAD_FILE_ID" // a file-id cannot be parsed
	BadLines   = "E_BAD_LINES"   // a --lines range cannot be parsed
	BadArgs    = "E_BAD_ARGS"    // a command got invalid arguments
	KlibFailed = "E_KLIB_FAILED" // the klib tool exited with an error
	DumpFormat = "E_DUMP_FORMAT" // klib output does not look like a metadata dump
	RgFailed   = "E_RG_FAILED"   // rg exited with an error
	Cache      = "E_CACHE"       // the cache directory cannot be used
)

// Error is a failure with a stable code and an optional hint.
type Error struct {
	Code    string
	Message string
	Hint    string
	Cause   error
}

// New returns a coded error. An empty hint is allowed.
func New(code, message, hint string) *Error {
	return &Error{Code: code, Message: message, Hint: hint}
}

// Wrap returns a coded error that keeps cause for errors.Is and errors.As.
func Wrap(cause error, code, message, hint string) *Error {
	return &Error{Code: code, Message: message, Hint: hint, Cause: cause}
}

// Error renders "E_CODE: message. Try: hint".
func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString(e.Code)
	b.WriteString(": ")
	b.WriteString(strings.TrimRight(e.Message, ". \n"))
	b.WriteString(".")
	if e.Hint != "" {
		b.WriteString(" Try: ")
		b.WriteString(e.Hint)
	}
	return b.String()
}

// Unwrap returns the underlying cause, if any.
func (e *Error) Unwrap() error { return e.Cause }

// Code returns the stable code of err, or "" when err is not a coded error.
func Code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}
