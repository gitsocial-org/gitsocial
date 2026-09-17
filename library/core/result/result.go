// result.go - Generic Result[T] pattern for extension APIs
package result

import "fmt"

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

// Text renders the error for display, with the details after the message.
func (e *Error) Text() string {
	if e == nil {
		return ""
	}
	if e.Details == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Details)
}
