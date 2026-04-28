// Package errors provides structured CLI errors with stable codes,
// actionable hints, and JSON serialization for stderr.
package errors

import (
	"encoding/json"
	"fmt"
	"io"
)

type Code string

const (
	CodeConfigMissing Code = "CONFIG_MISSING"
	CodeConfigInvalid Code = "CONFIG_INVALID"
	CodeFlagInvalid   Code = "FLAG_INVALID"
	CodeVMUnreachable Code = "VM_UNREACHABLE"
	CodeVMHTTP4XX     Code = "VM_HTTP_4XX"
	CodeVMHTTP5XX     Code = "VM_HTTP_5XX"
	CodeNoData        Code = "NO_DATA"
	CodeInternal      Code = "INTERNAL"
)

const (
	ExitOK        = 0
	ExitInternal  = 1
	ExitUserError = 2
	ExitVMFailure = 3
)

type CodedError struct {
	Code    Code
	Message string
	Hint    string
}

func (e *CodedError) Error() string { return e.Message }

func (e *CodedError) ExitCode() int {
	switch e.Code {
	case CodeConfigMissing, CodeConfigInvalid, CodeFlagInvalid:
		return ExitUserError
	case CodeVMUnreachable, CodeVMHTTP4XX, CodeVMHTTP5XX:
		return ExitVMFailure
	case CodeNoData:
		return ExitOK
	case CodeInternal:
		return ExitInternal
	default:
		return ExitInternal
	}
}

func Errorf(code Code, format string, args ...any) error {
	return &CodedError{Code: code, Message: fmt.Sprintf(format, args...)}
}

func WithHint(err error, hint string) error {
	if ce, ok := err.(*CodedError); ok {
		ce.Hint = hint
		return ce
	}
	return &CodedError{Code: CodeInternal, Message: err.Error(), Hint: hint}
}

type envelope struct {
	Error payload `json:"error"`
}

type payload struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

func WriteJSON(w io.Writer, err error) {
	ce, ok := err.(*CodedError)
	if !ok {
		ce = &CodedError{Code: CodeInternal, Message: err.Error()}
	}
	_ = json.NewEncoder(w).Encode(envelope{Error: payload{
		Code: ce.Code, Message: ce.Message, Hint: ce.Hint,
	}})
}

func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	if ce, ok := err.(*CodedError); ok {
		return ce.ExitCode()
	}
	return ExitInternal
}
