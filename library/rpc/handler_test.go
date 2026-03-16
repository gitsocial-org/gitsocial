// handler_test.go - Tests for method registry and dispatch
package rpc

import (
	"encoding/json"
	"testing"
)

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry() returned nil")
	}
}

func TestRegistry_Dispatch_methodNotFound(t *testing.T) {
	r := NewRegistry()
	_, err := r.Dispatch("nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for unknown method")
	}
	if err.Code != CodeMethodNotFound {
		t.Errorf("Code = %d, want %d", err.Code, CodeMethodNotFound)
	}
	if err.Message == "" {
		t.Error("Message should not be empty")
	}
}

func TestRegistry_RegisterAndDispatch(t *testing.T) {
	r := NewRegistry()
	r.Register("test.echo", func(params json.RawMessage) (any, *RPCError) {
		return "echoed", nil
	})

	result, err := r.Dispatch("test.echo", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "echoed" {
		t.Errorf("result = %v, want %q", result, "echoed")
	}
}

func TestRegistry_Dispatch_handlerError(t *testing.T) {
	r := NewRegistry()
	r.Register("test.fail", func(params json.RawMessage) (any, *RPCError) {
		return nil, &RPCError{Code: CodeAppInternal, Message: "something failed"}
	})

	result, err := r.Dispatch("test.fail", nil)
	if result != nil {
		t.Errorf("result = %v, want nil", result)
	}
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Code != CodeAppInternal {
		t.Errorf("Code = %d, want %d", err.Code, CodeAppInternal)
	}
}

func TestRegistry_Dispatch_withParams(t *testing.T) {
	r := NewRegistry()
	r.Register("test.add", func(params json.RawMessage) (any, *RPCError) {
		var p struct {
			A int `json:"a"`
			B int `json:"b"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &RPCError{Code: CodeInvalidParams, Message: err.Error()}
		}
		return p.A + p.B, nil
	})

	params := json.RawMessage(`{"a": 3, "b": 4}`)
	result, err := r.Dispatch("test.add", params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 7 {
		t.Errorf("result = %v, want 7", result)
	}
}

func TestRegistry_overwrite(t *testing.T) {
	r := NewRegistry()
	r.Register("test.method", func(params json.RawMessage) (any, *RPCError) {
		return "first", nil
	})
	r.Register("test.method", func(params json.RawMessage) (any, *RPCError) {
		return "second", nil
	})

	result, _ := r.Dispatch("test.method", nil)
	if result != "second" {
		t.Errorf("result = %v, want %q (overwritten)", result, "second")
	}
}
