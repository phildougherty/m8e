// internal/server/lifecycle.go
package server

import (
	"context"
	"sync"
)

// proxyLifecycle owns the proxy's cancellation context and background-goroutine
// WaitGroup. Keeping it separate stops lifecycle plumbing from being tangled up
// with caching and routing state.
type proxyLifecycle struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// newProxyLifecycle creates a lifecycle rooted at a cancellable background
// context.
func newProxyLifecycle() *proxyLifecycle {
	ctx, cancel := context.WithCancel(context.Background())

	return &proxyLifecycle{ctx: ctx, cancel: cancel}
}

// done returns the channel that closes when the proxy is shutting down.
func (l *proxyLifecycle) done() <-chan struct{} {
	return l.ctx.Done()
}

// goroutine runs fn as a tracked background goroutine.
func (l *proxyLifecycle) goroutine(fn func()) {
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		fn()
	}()
}

// shutdown cancels the context and waits for tracked goroutines to exit.
func (l *proxyLifecycle) shutdown() {
	l.cancel()
	l.wg.Wait()
}
