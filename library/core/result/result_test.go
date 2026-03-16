// result_test.go - Tests for generic Result[T] pattern
package result

import (
	"testing"
)

func TestOk(t *testing.T) {
	r := Ok("hello")
	if !r.Success {
		t.Error("Ok result should have Success=true")
	}
	if r.Data != "hello" {
		t.Errorf("Data = %q, want %q", r.Data, "hello")
	}
	if r.Error != nil {
		t.Error("Ok result should have nil Error")
	}
}

func TestOk_int(t *testing.T) {
	r := Ok(42)
	if !r.Success {
		t.Error("Ok result should have Success=true")
	}
	if r.Data != 42 {
		t.Errorf("Data = %d, want 42", r.Data)
	}
}

func TestOk_struct(t *testing.T) {
	type Item struct{ Name string }
	r := Ok(Item{Name: "test"})
	if r.Data.Name != "test" {
		t.Errorf("Data.Name = %q, want %q", r.Data.Name, "test")
	}
}

func TestOk_slice(t *testing.T) {
	r := Ok([]string{"a", "b"})
	if len(r.Data) != 2 {
		t.Errorf("Data length = %d, want 2", len(r.Data))
	}
}

func TestErr(t *testing.T) {
	r := Err[string]("NOT_FOUND", "item not found")
	if r.Success {
		t.Error("Err result should have Success=false")
	}
	if r.Error == nil {
		t.Fatal("Err result should have non-nil Error")
	}
	if r.Error.Code != "NOT_FOUND" {
		t.Errorf("Code = %q, want %q", r.Error.Code, "NOT_FOUND")
	}
	if r.Error.Message != "item not found" {
		t.Errorf("Message = %q, want %q", r.Error.Message, "item not found")
	}
	if r.Error.Details != nil {
		t.Error("Err should have nil Details")
	}
}

func TestErr_zeroValue(t *testing.T) {
	r := Err[int]("ERR", "failed")
	if r.Data != 0 {
		t.Errorf("Data should be zero value, got %d", r.Data)
	}
}

func TestErrWithDetails(t *testing.T) {
	details := map[string]string{"field": "email"}
	r := ErrWithDetails[string]("VALIDATION", "invalid input", details)
	if r.Success {
		t.Error("ErrWithDetails should have Success=false")
	}
	if r.Error == nil {
		t.Fatal("ErrWithDetails should have non-nil Error")
	}
	if r.Error.Code != "VALIDATION" {
		t.Errorf("Code = %q, want %q", r.Error.Code, "VALIDATION")
	}
	if r.Error.Message != "invalid input" {
		t.Errorf("Message = %q, want %q", r.Error.Message, "invalid input")
	}
	d, ok := r.Error.Details.(map[string]string)
	if !ok {
		t.Fatal("Details should be map[string]string")
	}
	if d["field"] != "email" {
		t.Errorf("Details[field] = %q, want %q", d["field"], "email")
	}
}

func TestErrWithDetails_nilDetails(t *testing.T) {
	r := ErrWithDetails[string]("ERR", "msg", nil)
	if r.Error.Details != nil {
		t.Error("Details should be nil when passed nil")
	}
}
