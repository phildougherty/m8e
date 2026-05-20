package mcp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// configTools owns the cluster configuration tools: apply_config (kubectl
// apply of a YAML blob) and reload_proxy (restart the proxy pod to pick up new
// servers).
type configTools struct {
	deps clusterDeps
}

func newConfigTools(deps clusterDeps) *configTools {
	return &configTools{deps: deps}
}

// applyConfig applies a YAML configuration to the cluster
func (c *configTools) applyConfig(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	configYAML, ok := args["config_yaml"].(string)
	if !ok || configYAML == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "config_yaml is required"}},
			IsError: true,
		}, fmt.Errorf("config_yaml is required")
	}

	// Write config to temporary file
	tempFile := filepath.Join(os.TempDir(), fmt.Sprintf("matey-config-%d.yaml", time.Now().Unix()))
	err := os.WriteFile(tempFile, []byte(configYAML), 0644)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error writing config file: %v", err)}},
			IsError: true,
		}, err
	}
	defer func() {
		if err := os.Remove(tempFile); err != nil {
			fmt.Printf("Warning: Failed to remove temp file %s: %v\n", tempFile, err)
		}
	}()

	// Apply using kubectl
	cmdArgs := []string{"apply", "-f", tempFile}
	if c.deps.namespace != "" {
		cmdArgs = append(cmdArgs, "-n", c.deps.namespace)
	}

	cmd := exec.CommandContext(ctx, "kubectl", cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error applying config: %v\nOutput: %s", err, string(output))}},
			IsError: true,
		}, err
	}

	return &ToolResult{
		Content: []Content{{Type: "text", Text: string(output)}},
	}, nil
}

// reloadProxy reloads MCP proxy configuration
func (c *configTools) reloadProxy(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	if !c.deps.useK8sClient() {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Kubernetes client not available. Cannot reload proxy."}},
			IsError: true,
		}, fmt.Errorf("kubernetes client not available")
	}

	// Find and restart the proxy pod to trigger reload
	var pods corev1.PodList
	labelSelector := client.MatchingLabels{"app": "matey-proxy"}
	err := c.deps.k8sClient.List(ctx, &pods, client.InNamespace(c.deps.namespace), labelSelector)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error finding proxy pods: %v", err)}},
			IsError: true,
		}, err
	}

	if len(pods.Items) == 0 {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "No proxy pods found. Proxy may not be running."}},
			IsError: true,
		}, fmt.Errorf("no proxy pods found")
	}

	// Delete the proxy pod to trigger restart and reload
	pod := &pods.Items[0]
	err = c.deps.k8sClient.Delete(ctx, pod)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error restarting proxy pod: %v", err)}},
			IsError: true,
		}, err
	}

	return &ToolResult{
		Content: []Content{{Type: "text", Text: fmt.Sprintf("Proxy pod %s restarted successfully. Configuration will be reloaded.", pod.Name)}},
	}, nil
}
