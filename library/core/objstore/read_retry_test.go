// read_retry_test.go - every bucket read and write retries a transient status inside do, and only once.
package objstore

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// faultClient returns a client pointed at addr, with the attempt count kept and the waits shrunk.
func faultClient(t *testing.T, addr string) *Client {
	t.Helper()
	client, err := NewClient(Config{Endpoint: "http://" + addr, Bucket: "b", Region: "us-east-1", AccessKey: "k", SecretKey: "s", PathStyle: true})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	client.retryBackoff = []time.Duration{time.Millisecond, time.Millisecond}
	return client
}

// TestGet_TransportErrorCostsOneRetryLevel: a failure carrying no status is transient, and costs do's attempts rather than a composed nine.
func TestGet_TransportErrorCostsOneRetryLevel(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var dials int64
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			atomic.AddInt64(&dials, 1)
			conn.Close()
		}
	}()

	client := faultClient(t, listener.Addr().String())
	if _, err := client.Get("k"); err == nil {
		t.Fatal("expected an error from a server that closes every connection")
	}
	want := int64(len(client.retryBackoff) + 1)
	if got := atomic.LoadInt64(&dials); got != want {
		t.Errorf("server saw %d dials, want %d (one retry level, not a composed one)", got, want)
	}
}

// TestGet_TruncatedBodyRetries: a 200 whose body ends early fails the read and is retried with its request.
func TestGet_TruncatedBodyRetries(t *testing.T) {
	var attempts int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&attempts, 1)
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		// A Content-Length the body never reaches, so io.ReadAll fails mid-stream.
		fmt.Fprint(buf, "HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\nshort")
		buf.Flush()
		conn.Close()
	}))
	defer srv.Close()

	client := faultClient(t, strings.TrimPrefix(srv.URL, "http://"))
	if _, err := client.Get("k"); err == nil {
		t.Fatal("expected an error from a body that ends before its Content-Length")
	}
	want := int64(len(client.retryBackoff) + 1)
	if got := atomic.LoadInt64(&attempts); got != want {
		t.Errorf("server saw %d requests, want %d (a body read that fails mid-stream retries with its request)", got, want)
	}
}

// TestGet_AbsorbsTransient500: a key whose first two GETs 500 still reads, and the retry hides the fault.
func TestGet_AbsorbsTransient500(t *testing.T) {
	client, bucket := testClient(t)
	if err := client.Put("k", []byte("value")); err != nil {
		t.Fatalf("put: %v", err)
	}
	bucket.FlakyGet("k", 2)
	got, err := client.Get("k")
	if err != nil {
		t.Fatalf("Get over a transient fault: %v", err)
	}
	if string(got) != "value" {
		t.Errorf("Get = %q, want %q", got, "value")
	}
	if n := bucket.GetCount("k"); n != 3 {
		t.Errorf("bucket saw %d GETs, want 3 (2 transient 500s + 1 success)", n)
	}
}

// TestGet_AbsorbsTransient503: a 503 is retried like any other 5xx.
func TestGet_AbsorbsTransient503(t *testing.T) {
	client, bucket := testClient(t)
	if err := client.Put("k", []byte("value")); err != nil {
		t.Fatalf("put: %v", err)
	}
	bucket.FlakyGetStatus("k", 1, 503)
	got, err := client.Get("k")
	if err != nil {
		t.Fatalf("Get over a 503: %v", err)
	}
	if string(got) != "value" {
		t.Errorf("Get = %q, want %q", got, "value")
	}
	if n := bucket.GetCount("k"); n != 2 {
		t.Errorf("bucket saw %d GETs, want 2 (one 503 + one success)", n)
	}
}

// TestPut_AbsorbsTransient503: a write retries a transient status too, so one throttled PUT does not fail a publish.
func TestPut_AbsorbsTransient503(t *testing.T) {
	client, bucket := testClient(t)
	bucket.FlakyPutStatus("k", 1, 503)
	if err := client.Put("k", []byte("value")); err != nil {
		t.Fatalf("Put over a 503: %v", err)
	}
	if body, ok := bucket.Object("k"); !ok || body != "value" {
		t.Errorf("stored %q (present %v), want %q", body, ok, "value")
	}
	if n := bucket.PutCount("k"); n != 1 {
		t.Errorf("bucket stored %d PUTs, want 1 (the 503 stored nothing)", n)
	}
}

// TestGet_GivesUpOnPersistentFault: a fault that never clears costs the attempts do allows, not a composed nine.
func TestGet_GivesUpOnPersistentFault(t *testing.T) {
	client, bucket := testClient(t)
	if err := client.Put("k", []byte("value")); err != nil {
		t.Fatalf("put: %v", err)
	}
	bucket.FailGet("k")
	if _, err := client.Get("k"); err == nil {
		t.Fatal("expected an error from a fault that never clears")
	}
	want := len(client.retryBackoff) + 1
	if n := bucket.GetCount("k"); n != want {
		t.Errorf("bucket saw %d GETs, want %d (one retry level, not a composed one)", n, want)
	}
}

// TestPut_GivesUpOnPersistentFault: a write that never succeeds costs the same bounded attempts.
func TestPut_GivesUpOnPersistentFault(t *testing.T) {
	client, bucket := testClient(t)
	bucket.FailPut("k")
	if err := client.Put("k", []byte("value")); err == nil {
		t.Fatal("expected an error from a PUT fault that never clears")
	}
	want := len(client.retryBackoff) + 1
	if n := bucket.PutAttempts("k"); n != want {
		t.Errorf("bucket saw %d PUT attempts, want %d (one retry level, not a composed one)", n, want)
	}
}

// TestGet_NoRetryOn404: a definite answer is not retried.
func TestGet_NoRetryOn404(t *testing.T) {
	client, bucket := testClient(t)
	_, err := client.Get("absent")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get on absent key = %v, want ErrNotFound", err)
	}
	if n := bucket.GetCount("absent"); n != 1 {
		t.Errorf("bucket saw %d GETs for a 404, want 1 (a 404 is a definite answer, never retried)", n)
	}
}

// TestPutIfAbsent_NoRetryOn412: contention is a caller's signal to re-read, never a fault to retry.
func TestPutIfAbsent_NoRetryOn412(t *testing.T) {
	client, bucket := testClient(t)
	if err := client.Put("k", []byte("first")); err != nil {
		t.Fatalf("put: %v", err)
	}
	before := bucket.PutAttempts("k")
	if err := client.PutIfAbsent("k", []byte("second")); !errors.Is(err, ErrPreconditionFailed) {
		t.Fatalf("PutIfAbsent over an existing key = %v, want ErrPreconditionFailed", err)
	}
	if n := bucket.PutAttempts("k") - before; n != 1 {
		t.Errorf("bucket saw %d conditional PUT attempts, want 1 (a 412 is never retried)", n)
	}
}
