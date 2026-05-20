package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestRootHelpExitsCleanly is the cheapest possible regression guard: build
// the root command, run --help, assert it doesn't panic and the output names
// the binary. A command that fails to construct (panic in constructor,
// nil-deref in init) would surface here as a test failure rather than as a
// runtime crash for an operator.
func TestRootHelpExitsCleanly(t *testing.T) {
	root := NewRootCommand("test")
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("matey --help failed: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "matey") {
		t.Errorf("matey --help output does not mention the binary name:\n%s", got)
	}
	// Every command surface that should be visible — fail loudly if the
	// grouping refactor drops one of these without replacement.
	for _, want := range []string{"up", "down", "ps", "logs", "events", "install", "validate", "proxy"} {
		if !strings.Contains(got, want) {
			t.Errorf("matey --help missing expected command %q in output", want)
		}
	}
}

// TestSubcommandHelpExitsCleanly walks every direct subcommand and runs `cmd
// --help`. A constructor that panics, a flag wired with a wrong default, a
// hidden command that errors on construction — anything that would crash
// before the operator even sees usage — shows up here as a failure rather
// than as a 2am incident.
func TestSubcommandHelpExitsCleanly(t *testing.T) {
	root := NewRootCommand("test")
	for _, sub := range root.Commands() {
		sub := sub
		t.Run(sub.Name(), func(t *testing.T) {
			out := &bytes.Buffer{}
			c := findCommand(root, sub.Name())
			if c == nil {
				t.Fatalf("could not re-find subcommand %q on fresh root", sub.Name())
			}
			c.Root().SetOut(out)
			c.Root().SetErr(out)
			c.Root().SetArgs([]string{sub.Name(), "--help"})
			if err := c.Root().Execute(); err != nil {
				t.Fatalf("matey %s --help failed: %v\n%s", sub.Name(), err, out.String())
			}
			if out.Len() == 0 {
				t.Errorf("matey %s --help produced no output", sub.Name())
			}
		})
	}
}

func findCommand(root *cobra.Command, name string) *cobra.Command {
	for _, c := range root.Commands() {
		if c.Name() == name {
			return c
		}
	}

	return nil
}
