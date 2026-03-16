// types.go - JSON-RPC 2.0 wire types, error codes, and result conversion helpers
package rpc

import (
	"encoding/json"

	"github.com/gitsocial-org/gitsocial/core/result"
)

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Standard JSON-RPC 2.0 error codes
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// Application error codes (-32000 to -32099)
const (
	CodeAppInternal    = -32000
	CodeNotFound       = -32001
	CodeNotARepository = -32002
	CodeNotInitialized = -32003
	CodeInvalidArg     = -32004
	CodePermission     = -32005
	CodeNetwork        = -32006
	CodeConflict       = -32007
	CodeNotReady       = -32010
)

// appErrorCode maps result.Error code strings to integer RPC error codes.
func appErrorCode(code string) int {
	switch code {
	case "NOT_FOUND":
		return CodeNotFound
	case "NOT_A_REPOSITORY":
		return CodeNotARepository
	case "NOT_INITIALIZED":
		return CodeNotInitialized
	case "INVALID_ARGUMENT":
		return CodeInvalidArg
	case "PERMISSION_DENIED":
		return CodePermission
	case "NETWORK_ERROR":
		return CodeNetwork
	case "CONFLICT":
		return CodeConflict
	default:
		return CodeAppInternal
	}
}

// appError creates an application-level RPC error with appCode in data.
func appError(code int, appCode, message string) *RPCError {
	return &RPCError{
		Code:    code,
		Message: message,
		Data:    map[string]any{"appCode": appCode},
	}
}

// fromResult converts a Result[T] to an RPC (data, error) pair.
func fromResult[T any](r result.Result[T]) (any, *RPCError) {
	if r.Success {
		return r.Data, nil
	}
	data := map[string]any{"appCode": r.Error.Code}
	if r.Error.Details != nil {
		data["details"] = r.Error.Details
	}
	return nil, &RPCError{
		Code:    appErrorCode(r.Error.Code),
		Message: r.Error.Message,
		Data:    data,
	}
}
