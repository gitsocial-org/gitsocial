// types_test.go - Tests for RPC type conversion and error mapping
package rpc

import (
	"testing"

	"github.com/gitsocial-org/gitsocial/core/result"
)

func TestAppErrorCode(t *testing.T) {
	tests := []struct {
		code string
		want int
	}{
		{"NOT_FOUND", CodeNotFound},
		{"NOT_A_REPOSITORY", CodeNotARepository},
		{"NOT_INITIALIZED", CodeNotInitialized},
		{"INVALID_ARGUMENT", CodeInvalidArg},
		{"PERMISSION_DENIED", CodePermission},
		{"NETWORK_ERROR", CodeNetwork},
		{"CONFLICT", CodeConflict},
		{"UNKNOWN", CodeAppInternal},
		{"", CodeAppInternal},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			got := appErrorCode(tt.code)
			if got != tt.want {
				t.Errorf("appErrorCode(%q) = %d, want %d", tt.code, got, tt.want)
			}
		})
	}
}

func TestAppError(t *testing.T) {
	err := appError(CodeNotFound, "NOT_FOUND", "item not found")
	if err.Code != CodeNotFound {
		t.Errorf("Code = %d, want %d", err.Code, CodeNotFound)
	}
	if err.Message != "item not found" {
		t.Errorf("Message = %q", err.Message)
	}
	data, ok := err.Data.(map[string]any)
	if !ok {
		t.Fatal("Data should be map[string]any")
	}
	if data["appCode"] != "NOT_FOUND" {
		t.Errorf("appCode = %v", data["appCode"])
	}
}

func TestFromResult_success(t *testing.T) {
	r := result.Ok("hello")
	data, rpcErr := fromResult(r)
	if rpcErr != nil {
		t.Fatalf("unexpected error: %v", rpcErr)
	}
	if data != "hello" {
		t.Errorf("data = %v, want %q", data, "hello")
	}
}

func TestFromResult_error(t *testing.T) {
	r := result.Err[string]("NOT_FOUND", "item not found")
	data, rpcErr := fromResult(r)
	if data != nil {
		t.Errorf("data = %v, want nil", data)
	}
	if rpcErr == nil {
		t.Fatal("expected error")
	}
	if rpcErr.Code != CodeNotFound {
		t.Errorf("Code = %d, want %d", rpcErr.Code, CodeNotFound)
	}
	if rpcErr.Message != "item not found" {
		t.Errorf("Message = %q", rpcErr.Message)
	}
	rpcData, ok := rpcErr.Data.(map[string]any)
	if !ok {
		t.Fatal("error Data should be map[string]any")
	}
	if rpcData["appCode"] != "NOT_FOUND" {
		t.Errorf("appCode = %v", rpcData["appCode"])
	}
}

func TestFromResult_errorWithDetails(t *testing.T) {
	r := result.ErrWithDetails[string]("INVALID_ARGUMENT", "bad input", map[string]string{"field": "name"})
	_, rpcErr := fromResult(r)
	if rpcErr == nil {
		t.Fatal("expected error")
	}
	rpcData, ok := rpcErr.Data.(map[string]any)
	if !ok {
		t.Fatal("error Data should be map[string]any")
	}
	if rpcData["details"] == nil {
		t.Error("details should not be nil")
	}
}

func TestFromResult_successInt(t *testing.T) {
	r := result.Ok(42)
	data, rpcErr := fromResult(r)
	if rpcErr != nil {
		t.Fatalf("unexpected error: %v", rpcErr)
	}
	if data != 42 {
		t.Errorf("data = %v, want 42", data)
	}
}

func TestFromResult_successStruct(t *testing.T) {
	type Item struct {
		ID   string
		Name string
	}
	r := result.Ok(Item{ID: "1", Name: "test"})
	data, rpcErr := fromResult(r)
	if rpcErr != nil {
		t.Fatalf("unexpected error: %v", rpcErr)
	}
	item, ok := data.(Item)
	if !ok {
		t.Fatal("data should be Item")
	}
	if item.ID != "1" {
		t.Errorf("ID = %q", item.ID)
	}
}
