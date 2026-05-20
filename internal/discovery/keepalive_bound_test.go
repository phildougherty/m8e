// internal/discovery/keepalive_bound_test.go
package discovery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/phildougherty/m8e/internal/logging"
)

// TestPerformKeepAlive_BoundedConcurrency verifies that performKeepAlive does
// not spawn an unbounded fan-out of goroutines when there are many active
// connections and the upstream keep-alive endpoint is slow. The previous code
// spawned one goroutine per connection per tick, which on a stalled upstream
// would pile up goroutines faster than they could drain.
func TestPerformKeepAlive_BoundedConcurrency(t *testing.T) {
	var (
		inFlight    int32
		maxInFlight int32
	)

	// Slow upstream: each keep-alive ping blocks long enough that all
	// connections cannot complete in serial, so we can observe whether the
	// semaphore is actually limiting concurrency.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := atomic.AddInt32(&inFlight, 1)
		for {
			prev := atomic.LoadInt32(&maxInFlight)
			if cur <= prev || atomic.CompareAndSwapInt32(&maxInFlight, prev, cur) {
				break
			}
		}
		// Hold the request briefly so concurrency can actually build up.
		time.Sleep(80 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	logger := logging.NewLogger("error")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dcm := &DynamicConnectionManager{
		connections:            make(map[string]*MCPConnection),
		logger:                 logger,
		ctx:                    ctx,
		cancel:                 cancel,
		healthTimeout:          5 * time.Second,
		maxConsecutiveFailures: 3,
	}

	// Register far more connections than the bound so any unbounded fan-out
	// would be obvious. 64 connections vs a bound of 16 == 4x oversubscription.
	const numConns = 64
	for i := 0; i < numConns; i++ {
		name := "conn-" + string(rune('a'+(i%26))) + string(rune('0'+(i/26)))
		dcm.connections[name] = &MCPConnection{
			Name:     name,
			Protocol: "http",
			Status:   "connected",
			HTTPConnection: &MCPHTTPConnection{
				BaseURL: srv.URL,
				Client:  &http.Client{Timeout: 5 * time.Second},
			},
		}
	}

	// Run performKeepAlive concurrently with a sampler that records the
	// observed concurrent-goroutine ceiling. We sample inFlight (not
	// runtime.NumGoroutine, which is noisy across packages) because it is
	// the precise quantity we care about: the number of keep-alive RPCs
	// running at the same time.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		dcm.performKeepAlive()
	}()

	wg.Wait()

	got := atomic.LoadInt32(&maxInFlight)
	if got > int32(maxConcurrentKeepAlives) {
		t.Fatalf("max concurrent keep-alives observed = %d, bound = %d", got, maxConcurrentKeepAlives)
	}
	if got == 0 {
		t.Fatalf("max concurrent keep-alives observed = 0; test server was never reached")
	}
	t.Logf("max concurrent keep-alives observed = %d (bound = %d, connections = %d)", got, maxConcurrentKeepAlives, numConns)
}
