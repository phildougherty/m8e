// internal/ai/providers_body_test.go
package ai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// countingReadCloser wraps the error-body payload returned by a mock AI
// endpoint and records reads / closes. We use it to assert that the provider
// fully drains the body (so the connection can be returned to the pool) and
// closes it (so the goroutine-leak risk is eliminated) on the HTTP-error
// path. The original code ignored io.ReadAll's error and relied on the
// deferred close running after function return.
type countingReadCloser struct {
	r         io.Reader
	readCount int32
	closed    int32
	readErr   atomic.Value // error to inject after the first Read, if non-nil
}

func (c *countingReadCloser) Read(p []byte) (int, error) {
	atomic.AddInt32(&c.readCount, 1)
	if e, _ := c.readErr.Load().(error); e != nil {
		return 0, e
	}
	return c.r.Read(p)
}

func (c *countingReadCloser) Close() error {
	atomic.StoreInt32(&c.closed, 1)
	return nil
}

// mockErrorServer returns HTTP 500 with the given body. We wrap the writer's
// body via a hijacked transport rather than httptest so that we can observe
// the *client-side* body lifecycle directly; httptest gives us no hook on
// the response body the client receives. Instead we use a RoundTripper
// surrogate.
type spyTransport struct {
	body *countingReadCloser
	hdr  http.Header
	code int
}

func (s *spyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: s.code,
		Body:       s.body,
		Header:     s.hdr,
		Request:    req,
		ProtoMajor: 1,
		ProtoMinor: 1,
	}, nil
}

func newSpyTransport(body string) *spyTransport {
	return &spyTransport{
		body: &countingReadCloser{r: strings.NewReader(body)},
		hdr:  http.Header{"Content-Type": []string{"application/json"}},
		code: http.StatusInternalServerError,
	}
}

// waitForClose polls until the body has been closed or the deadline fires.
// The providers do their work in a goroutine so the test must give it a
// moment to drive the StatusCode != 200 branch.
func waitForClose(c *countingReadCloser, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&c.closed) == 1 {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return atomic.LoadInt32(&c.closed) == 1
}

func TestOpenAIProvider_ErrorBodyDrainedAndClosed(t *testing.T) {
	// httptest server is used only to give NewOpenAIProvider a valid endpoint;
	// the spy transport intercepts the actual request.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	spy := newSpyTransport(`{"error":"boom"}`)
	p, err := NewOpenAIProvider(ProviderConfig{
		APIKey:   "test-key",
		Endpoint: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewOpenAIProvider: %v", err)
	}
	p.httpClient = &http.Client{Transport: spy}

	ch, err := p.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, StreamOptions{})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}

	// Drain the channel so the streaming goroutine reaches its return path.
	for resp := range ch {
		if resp.Error == nil {
			t.Logf("non-error response: %+v", resp)
		}
	}

	if !waitForClose(spy.body, time.Second) {
		t.Fatalf("response body was not closed on HTTP-error path")
	}
	if atomic.LoadInt32(&spy.body.readCount) == 0 {
		t.Fatalf("response body was never read on HTTP-error path; body must be drained so the connection can be reused")
	}
}

func TestOllamaProvider_ErrorBodyDrainedAndClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	spy := newSpyTransport(`{"error":"down"}`)
	p, err := NewOllamaProvider(ProviderConfig{Endpoint: srv.URL})
	if err != nil {
		t.Fatalf("NewOllamaProvider: %v", err)
	}
	p.httpClient = &http.Client{Transport: spy}

	ch, err := p.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, StreamOptions{})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	for range ch {
	}

	if !waitForClose(spy.body, time.Second) {
		t.Fatalf("response body was not closed on HTTP-error path")
	}
	if atomic.LoadInt32(&spy.body.readCount) == 0 {
		t.Fatalf("response body was never read on HTTP-error path")
	}
}

func TestClaudeProvider_ErrorBodyDrainedAndClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	spy := newSpyTransport(`{"error":"unauthorized"}`)
	p, err := NewClaudeProvider(ProviderConfig{APIKey: "test-key", Endpoint: srv.URL})
	if err != nil {
		t.Fatalf("NewClaudeProvider: %v", err)
	}
	p.httpClient = &http.Client{Transport: spy}

	ch, err := p.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, StreamOptions{})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	for range ch {
	}

	if !waitForClose(spy.body, time.Second) {
		t.Fatalf("response body was not closed on HTTP-error path")
	}
	if atomic.LoadInt32(&spy.body.readCount) == 0 {
		t.Fatalf("response body was never read on HTTP-error path")
	}
}

func TestOpenRouterProvider_ErrorBodyDrainedAndClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	spy := newSpyTransport(`{"error":"rate limited"}`)
	p, err := NewOpenRouterProvider(ProviderConfig{APIKey: "test-key", Endpoint: srv.URL})
	if err != nil {
		t.Fatalf("NewOpenRouterProvider: %v", err)
	}
	p.httpClient = &http.Client{Transport: spy}

	ch, err := p.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, StreamOptions{})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	for range ch {
	}

	if !waitForClose(spy.body, time.Second) {
		t.Fatalf("response body was not closed on HTTP-error path")
	}
	if atomic.LoadInt32(&spy.body.readCount) == 0 {
		t.Fatalf("response body was never read on HTTP-error path")
	}
}
