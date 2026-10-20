// result.go - Generic Result[T] pattern for extension APIs
package result

import (
	"fmt"
	"strings"
)

type Result[T any] struct {
	Success bool
	Data    T
	Error   *Error
}

type Error struct {
	Code    string
	Message string
	Details interface{}
}

// Ok creates a successful Result with the given data.
func Ok[T any](data T) Result[T] {
	return Result[T]{Success: true, Data: data}
}

// Err creates a failed Result with an error code and message.
func Err[T any](code, message string) Result[T] {
	return Result[T]{
		Success: false,
		Error:   &Error{Code: code, Message: message},
	}
}

// ErrWithDetails creates a failed Result with additional error details.
func ErrWithDetails[T any](code, message string, details interface{}) Result[T] {
	return Result[T]{
		Success: false,
		Error:   &Error{Code: code, Message: message, Details: details},
	}
}

// ErrFrom creates a failed Result that carries the error of another result.
func ErrFrom[T any](e *Error) Result[T] {
	return Result[T]{Success: false, Error: e}
}

// Text renders the error for display, with the details on one line after the message.
func (e *Error) Text() string {
	if e == nil {
		return ""
	}
	if e.Details == nil {
		return e.Message
	}
	details := oneLine(fmt.Sprint(e.Details))
	if details == "" {
		return e.Message
	}
	return e.Message + ": " + details
}

// oneLine joins the non-empty trimmed lines of s with "; ".
func oneLine(s string) string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "; ")
}
