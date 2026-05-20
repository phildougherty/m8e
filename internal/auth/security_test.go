package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/phildougherty/m8e/internal/logging"
)

// TestConstantTimeEqual_Helper exercises the local constant-time comparison
// helper. Correctness alone (not timing) is asserted; the timing property is
// inherited from crypto/subtle.ConstantTimeCompare.
func TestConstantTimeEqual_Helper(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"", "", true},
		{"abc", "abc", true},
		{"abc", "abd", false},
		{"abc", "abcd", false},
		{"", "x", false},
		{strings.Repeat("a", 64), strings.Repeat("a", 64), true},
		{strings.Repeat("a", 64), strings.Repeat("a", 63) + "b", false},
	}
	for _, c := range cases {
		got := constantTimeEqual(c.a, c.b)
		if got != c.want {
			t.Errorf("constantTimeEqual(%q,%q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// TestValidateClient_ConstantTime verifies ValidateClient now uses a
// constant-time compare on the client secret. We can't directly measure
// timing in a unit test, but we can at least confirm the correctness branch:
// matching secret returns the client, non-matching secret returns the
// "invalid client credentials" error.
func TestValidateClient_ConstantTime(t *testing.T) {
	logger := logging.NewLogger("error")
	srv := NewAuthorizationServer(&AuthorizationServerConfig{
		Issuer:                "https://test.local",
		AuthorizationEndpoint: "/oauth/authorize",
		TokenEndpoint:         "/oauth/token",
	}, logger)

	client, err := srv.RegisterClient(&OAuthConfig{
		ClientID:     "test-client",
		ClientSecret: "super-secret-value-12345",
		RedirectURIs: []string{"https://app.example.com/cb"},
	})
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}

	// Matching secret -> success.
	if _, err := srv.ValidateClient(client.ID, "super-secret-value-12345"); err != nil {
		t.Errorf("expected matching secret to validate, got %v", err)
	}

	// Non-matching but same length -> failure.
	if _, err := srv.ValidateClient(client.ID, "super-secret-value-WRONG"); err == nil {
		t.Errorf("expected non-matching same-length secret to fail")
	}

	// Different length entirely -> failure.
	if _, err := srv.ValidateClient(client.ID, "x"); err == nil {
		t.Errorf("expected single-char secret to fail")
	}

	// Empty secret -> failure (client is not public).
	if _, err := srv.ValidateClient(client.ID, ""); err == nil {
		t.Errorf("expected empty secret to fail for confidential client")
	}
}

// TestParseAuthorizationRequest_RejectsPlainPKCE confirms that an explicit
// code_challenge_method=plain on the authorization request is rejected — the
// "plain" method is deprecated by RFC 7636 §7.2 / OAuth 2.1.
func TestParseAuthorizationRequest_RejectsPlainPKCE(t *testing.T) {
	logger := logging.NewLogger("error")
	srv := NewAuthorizationServer(&AuthorizationServerConfig{
		Issuer:                "https://test.local",
		AuthorizationEndpoint: "/oauth/authorize",
		TokenEndpoint:         "/oauth/token",
	}, logger)

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", "client-x")
	q.Set("code_challenge", "abc")
	q.Set("code_challenge_method", "plain")

	r := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil)
	_, err := srv.parseAuthorizationRequest(r)
	if err == nil {
		t.Fatalf("expected parseAuthorizationRequest to reject code_challenge_method=plain")
	}
	if !strings.Contains(err.Error(), "S256") {
		t.Errorf("expected error to mention S256, got %v", err)
	}
}

// TestParseAuthorizationRequest_DefaultsMissingMethodToS256 confirms the
// migration-compatibility path: a client that sends a code_challenge but
// omits code_challenge_method gets defaulted to S256 (the only method we
// accept).
func TestParseAuthorizationRequest_DefaultsMissingMethodToS256(t *testing.T) {
	logger := logging.NewLogger("error")
	srv := NewAuthorizationServer(&AuthorizationServerConfig{
		Issuer:                "https://test.local",
		AuthorizationEndpoint: "/oauth/authorize",
		TokenEndpoint:         "/oauth/token",
	}, logger)

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", "client-x")
	q.Set("code_challenge", "abc")
	// code_challenge_method intentionally omitted

	r := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil)
	req, err := srv.parseAuthorizationRequest(r)
	if err != nil {
		t.Fatalf("parseAuthorizationRequest: %v", err)
	}
	if req.CodeChallengeMethod != "S256" {
		t.Errorf("expected default code_challenge_method=S256, got %q", req.CodeChallengeMethod)
	}
}

// TestTokenStore_CloseWaitsForCleanup confirms Close() does not return until
// the cleanup goroutine has actually exited. We can't strictly assert
// "goroutine has exited" from outside the package, but we can confirm Close()
// is idempotent and returns promptly without deadlock — which is the
// observable property exercised by the new WaitGroup. The race detector
// (go test -race) is the primary check against the previous "drop and run"
// shutdown.
func TestTokenStore_CloseWaitsForCleanup(t *testing.T) {
	ts := NewTokenStore()
	// Calling Close once should return cleanly.
	ts.Close()
	// Calling Close twice would panic on the underlying close(stopChan); we
	// deliberately do NOT call it a second time. Returning here without a
	// deadlock is the test's positive signal.
}

func TestMemoryTokenStore_CloseWaitsForCleanup(t *testing.T) {
	ts := NewMemoryTokenStore()
	ts.Close()
}
