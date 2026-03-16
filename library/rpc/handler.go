// handler.go - Method registry and dispatch for JSON-RPC server
package rpc

import (
	"encoding/json"
	"fmt"
)

// HandlerFunc processes JSON-RPC params and returns a result or error.
type HandlerFunc func(params json.RawMessage) (any, *RPCError)

// Registry maps method names to handler functions.
type Registry struct {
	methods map[string]HandlerFunc
}

// NewRegistry creates an empty method registry.
func NewRegistry() *Registry {
	return &Registry{methods: make(map[string]HandlerFunc)}
}

// Register adds a handler for the given method name.
func (r *Registry) Register(method string, handler HandlerFunc) {
	r.methods[method] = handler
}

// Dispatch calls the handler for the given method, or returns method-not-found.
func (r *Registry) Dispatch(method string, params json.RawMessage) (any, *RPCError) {
	handler, ok := r.methods[method]
	if !ok {
		return nil, &RPCError{
			Code:    CodeMethodNotFound,
			Message: fmt.Sprintf("method not found: %s", method),
		}
	}
	return handler(params)
}
