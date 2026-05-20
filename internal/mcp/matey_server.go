package mcp

import (
	"github.com/phildougherty/m8e/internal/compose"
	"github.com/phildougherty/m8e/internal/memory"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// clusterDeps is the set of Kubernetes/compose handles every tool manager
// needs. It is populated once during construction and injected by value into
// each manager so no manager reaches back into the top-level server.
type clusterDeps struct {
	mateyBinary string
	configFile  string
	namespace   string
	k8sClient   client.Client
	clientset   kubernetes.Interface
	composer    *compose.K8sComposer
	config      *rest.Config
}

// useK8sClient returns true if the Kubernetes client and composer are both
// available. Managers share this gate so behaviour stays identical to the
// pre-refactor single-struct implementation.
func (d clusterDeps) useK8sClient() bool {
	return d.k8sClient != nil && d.composer != nil
}

// MateyMCPServer provides MCP tools for interacting with Matey and the cluster.
//
// It is a thin facade: its job is construction/wiring, owning the tool
// registry (matey_server_tools.go), and dispatching ExecuteTool calls to one
// of the focused managers below. Each manager owns one cohesive subdomain.
type MateyMCPServer struct {
	deps clusterDeps

	cluster   *clusterTools   // matey_ps/up/down, logs, inspect, cluster state
	memory    *memoryTools    // knowledge-graph entity/relation tools
	workflows *workflowTools  // workflow CRUD + execution history
	workspace *workspaceTools // workspace files, search_in_files, todos
	config    *configTools    // apply_config, reload_proxy
	agent     *agentRunner    // execute_agent LLM sub-agent
	mention   *mentionTools   // process_mentions, expand_mentions

	bashPolicy BashPolicy // Governs the execute_bash tool; see bash_policy.go
}

// Tool represents an MCP tool
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

// ToolResult represents the result of a tool execution
type ToolResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

// Content represents content in a tool result
type Content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// memoryHandles bundles the optional memory store/tools so the memory manager
// can be (re)wired once the memory service is discovered.
type memoryHandles struct {
	store *memory.MemoryStore
	tools *memory.MCPMemoryTools
}
