package cmd

import (
	"context"
	"testing"
	"time"
)

func TestNewPsCommand(t *testing.T) {
	cmd := NewPsCommand()

	if cmd == nil {
		t.Fatal("NewPsCommand() returned nil")
	}

	if cmd.Use != "ps" {
		t.Errorf("Expected Use to be 'ps', got %s", cmd.Use)
	}

	if cmd.Short != "Show running MCP servers with detailed process information" {
		t.Errorf("Expected correct Short description, got %s", cmd.Short)
	}

	if cmd.RunE == nil {
		t.Error("RunE should not be nil")
	}

	if cmd.Long == "" {
		t.Error("Long description should not be empty")
	}
}

func TestPsCommandFlags(t *testing.T) {
	cmd := NewPsCommand()

	// Check that flags exist
	if !cmd.Flags().HasFlags() {
		t.Error("Command should have flags")
	}

	// Check specific flags
	if flag := cmd.Flags().Lookup("watch"); flag == nil {
		t.Error("Should have 'watch' flag")
	}

	if flag := cmd.Flags().Lookup("format"); flag == nil {
		t.Error("Should have 'format' flag")
	}

	if flag := cmd.Flags().Lookup("filter"); flag == nil {
		t.Error("Should have 'filter' flag")
	}

	if flag := cmd.Flags().Lookup("interval"); flag == nil {
		t.Error("Should have 'interval' flag")
	} else if flag.DefValue != "2s" {
		t.Errorf("Expected interval default '2s', got %s", flag.DefValue)
	}
}

// TestRunPsWatchRespectsContextCancellation verifies the watch loop exits
// promptly when its context is cancelled rather than hanging on the ticker.
func TestRunPsWatchRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the loop starts

	done := make(chan error, 1)
	go func() {
		// A long interval ensures we are not just racing the ticker: the loop
		// must return because of ctx.Done(), not a tick. The gather call will
		// likely error without a cluster, but that must not abort the loop.
		done <- runPsWatch(ctx, "/nonexistent/matey.yaml", time.Hour)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("runPsWatch returned error on cancellation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runPsWatch did not return after context cancellation")
	}
}

// TestRunPsWatchCancelMidLoop verifies cancellation during an active loop also
// returns promptly.
func TestRunPsWatchCancelMidLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- runPsWatch(ctx, "/nonexistent/matey.yaml", 50*time.Millisecond)
	}()

	time.Sleep(120 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("runPsWatch returned error on cancellation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runPsWatch did not return after mid-loop cancellation")
	}
}
