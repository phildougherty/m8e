package mcp

import (
	"context"
	"os/exec"
)

// runMateyCommand runs a matey command and returns the output (fallback for unsupported operations)
func (m *MateyMCPServer) runMateyCommand(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, m.mateyBinary, args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}