// sigv4_test.go - SigV4 signing verified against the AWS S3 documentation example
package objstore

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestSignRequest_s3DocExample reproduces the "GET Bucket Lifecycle" worked
// example from the S3 SigV4 documentation (Authenticating Requests: Using the
// Authorization Header) and asserts the documented signature.
func TestSignRequest_s3DocExample(t *testing.T) {
	req, err := http.NewRequest("GET", "https://examplebucket.s3.amazonaws.com/?lifecycle", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)
	signRequest(req,
		"AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		"us-east-1", "s3", emptyPayloadSHA256, now)

	auth := req.Header.Get("Authorization")
	wantSig := "Signature=fea454ca298b7da1c68078a5d1bdbfbbe0d65c699e0f91ac7a200a0136783543"
	if !strings.Contains(auth, wantSig) {
		t.Errorf("Authorization = %q\nwant it to contain %q", auth, wantSig)
	}
	wantCred := "Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request"
	if !strings.Contains(auth, wantCred) {
		t.Errorf("Authorization = %q\nwant it to contain %q", auth, wantCred)
	}
	if !strings.Contains(auth, "SignedHeaders=host;x-amz-content-sha256;x-amz-date") {
		t.Errorf("Authorization = %q\nwant host;x-amz-content-sha256;x-amz-date signed", auth)
	}
}
