// server_test.go - Tests for JSON-RPC server utilities
package rpc

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestIsNotification(t *testing.T) {
	tests := []struct {
		name string
		id   json.RawMessage
		want bool
	}{
		{"nil id", nil, true},
		{"empty id", json.RawMessage{}, true},
		{"numeric id", json.RawMessage(`1`), false},
		{"string id", json.RawMessage(`"abc"`), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isNotification(tt.id)
			if got != tt.want {
				t.Errorf("isNotification(%v) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

func TestProcessRequest_invalidVersion(t *testing.T) {
	r := NewRegistry()
	s := NewServer(r, bytes.NewReader(nil), &bytes.Buffer{})

	resp := s.processRequest(Request{
		JSONRPC: "1.0",
		ID:      json.RawMessage(`1`),
		Method:  "test",
	})
	if resp.Error == nil {
		t.Fatal("expected error for invalid version")
	}
	if resp.Error.Code != CodeInvalidRequest {
		t.Errorf("Code = %d, want %d", resp.Error.Code, CodeInvalidRequest)
	}
}

func TestProcessRequest_emptyMethod(t *testing.T) {
	r := NewRegistry()
	s := NewServer(r, bytes.NewReader(nil), &bytes.Buffer{})

	resp := s.processRequest(Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "",
	})
	if resp.Error == nil {
		t.Fatal("expected error for empty method")
	}
	if resp.Error.Code != CodeInvalidRequest {
		t.Errorf("Code = %d, want %d", resp.Error.Code, CodeInvalidRequest)
	}
}

func TestProcessRequest_methodNotFound(t *testing.T) {
	r := NewRegistry()
	s := NewServer(r, bytes.NewReader(nil), &bytes.Buffer{})

	resp := s.processRequest(Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "nonexistent",
	})
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != CodeMethodNotFound {
		t.Errorf("Code = %d, want %d", resp.Error.Code, CodeMethodNotFound)
	}
}

func TestProcessRequest_success(t *testing.T) {
	r := NewRegistry()
	r.Register("test.ping", func(params json.RawMessage) (any, *RPCError) {
		return "pong", nil
	})
	s := NewServer(r, bytes.NewReader(nil), &bytes.Buffer{})

	resp := s.processRequest(Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "test.ping",
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	if resp.Result != "pong" {
		t.Errorf("Result = %v, want %q", resp.Result, "pong")
	}
	if resp.JSONRPC != "2.0" {
		t.Errorf("JSONRPC = %q", resp.JSONRPC)
	}
}

func TestDecodeParams_empty(t *testing.T) {
	type P struct {
		Name string `json:"name"`
	}
	p, err := decodeParams[P](nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name != "" {
		t.Errorf("Name = %q, want empty", p.Name)
	}
}

func TestDecodeParams_null(t *testing.T) {
	type P struct {
		Name string `json:"name"`
	}
	p, err := decodeParams[P](json.RawMessage(`null`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name != "" {
		t.Errorf("Name = %q, want empty", p.Name)
	}
}

func TestDecodeParams_valid(t *testing.T) {
	type P struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	p, err := decodeParams[P](json.RawMessage(`{"name":"Alice","age":30}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name != "Alice" {
		t.Errorf("Name = %q, want %q", p.Name, "Alice")
	}
	if p.Age != 30 {
		t.Errorf("Age = %d, want 30", p.Age)
	}
}

func TestDecodeParams_invalidJSON(t *testing.T) {
	type P struct {
		Name string `json:"name"`
	}
	_, err := decodeParams[P](json.RawMessage(`{invalid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if err.Code != CodeInvalidParams {
		t.Errorf("Code = %d, want %d", err.Code, CodeInvalidParams)
	}
}

func TestNewServer(t *testing.T) {
	r := NewRegistry()
	var buf bytes.Buffer
	s := NewServer(r, bytes.NewReader(nil), &buf)
	if s == nil {
		t.Fatal("NewServer returned nil")
	}
	if s.session == nil {
		t.Error("session should not be nil")
	}
}

func TestRequireInit_notInitialized(t *testing.T) {
	r := NewRegistry()
	s := NewServer(r, bytes.NewReader(nil), &bytes.Buffer{})

	handler := s.requireInit(func(params json.RawMessage) (any, *RPCError) {
		return "ok", nil
	})
	_, err := handler(nil)
	if err == nil {
		t.Fatal("expected error when not initialized")
	}
	if err.Code != CodeNotReady {
		t.Errorf("Code = %d, want %d", err.Code, CodeNotReady)
	}
}

func TestRequireInit_initialized(t *testing.T) {
	r := NewRegistry()
	s := NewServer(r, bytes.NewReader(nil), &bytes.Buffer{})
	s.session.Initialized = true

	handler := s.requireInit(func(params json.RawMessage) (any, *RPCError) {
		return "ok", nil
	})
	result, err := handler(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("result = %v, want %q", result, "ok")
	}
}
