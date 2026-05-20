// internal/protocol/resources_hash_test.go
package protocol

import (
	"testing"
)

// TestHashHex_SHA256Properties verifies the content-hash helper returns a
// deterministic, 64-hex-char SHA-256 digest. The hash is consumed as an
// integrity / cache key (not for authentication); these properties matter
// to callers that compare hashes across requests.
func TestHashHex_SHA256Properties(t *testing.T) {
	want := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" // sha256("")
	if got := hashHex(nil); got != want {
		t.Errorf("hashHex(nil) = %q, want %q", got, want)
	}
	if got := hashHex([]byte("")); got != want {
		t.Errorf("hashHex(empty) = %q, want %q", got, want)
	}

	a := hashHex([]byte("hello"))
	b := hashHex([]byte("hello"))
	if a != b {
		t.Errorf("hashHex not deterministic: %q vs %q", a, b)
	}
	if len(a) != 64 {
		t.Errorf("expected 64-char SHA-256 hex, got %d chars: %q", len(a), a)
	}

	if hashHex([]byte("hello")) == hashHex([]byte("world")) {
		t.Errorf("distinct inputs produced identical hashes (unexpected collision)")
	}
}

// TestResourceManager_GenerateContentHash exercises the public path that
// previously used MD5 so the hash output is now SHA-256.
func TestResourceManager_GenerateContentHash(t *testing.T) {
	rm := &ResourceManager{}
	h := rm.generateContentHash("hello")
	if len(h) != 64 {
		t.Errorf("expected 64-char hex digest, got %d: %q", len(h), h)
	}
	// "hello" sha256:
	want := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if h != want {
		t.Errorf("generateContentHash(\"hello\") = %q, want %q", h, want)
	}
}
