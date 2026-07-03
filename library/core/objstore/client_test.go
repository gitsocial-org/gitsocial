// client_test.go - Credential resolution tests
package objstore

import (
	"strings"
	"testing"
)

func TestNewClient_credentialPrecedence(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "aws-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "aws-secret")
	t.Setenv("GITSOCIAL_S3_ACCESS_KEY", "gs-key")
	t.Setenv("GITSOCIAL_S3_SECRET_KEY", "gs-secret")

	client, err := NewClient(Config{Bucket: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if client.cfg.AccessKey != "gs-key" || client.cfg.SecretKey != "gs-secret" {
		t.Errorf("creds = (%s, %s), want the GITSOCIAL_S3_* overrides", client.cfg.AccessKey, client.cfg.SecretKey)
	}

	// Explicit config still wins over both.
	client, err = NewClient(Config{Bucket: "b", AccessKey: "explicit", SecretKey: "explicit-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if client.cfg.AccessKey != "explicit" {
		t.Errorf("explicit config overridden by env: %s", client.cfg.AccessKey)
	}
}

func TestNewClient_credentialFallbackAndError(t *testing.T) {
	t.Setenv("GITSOCIAL_S3_ACCESS_KEY", "")
	t.Setenv("GITSOCIAL_S3_SECRET_KEY", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "aws-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "aws-secret")

	client, err := NewClient(Config{Bucket: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if client.cfg.AccessKey != "aws-key" {
		t.Errorf("fallback creds = %s, want the AWS_* pair", client.cfg.AccessKey)
	}

	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	_, err = NewClient(Config{Bucket: "b"})
	if err == nil || !strings.Contains(err.Error(), "GITSOCIAL_S3_ACCESS_KEY") {
		t.Errorf("missing-credentials error = %v, want it to name both variable sets", err)
	}
}
