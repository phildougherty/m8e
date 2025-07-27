package mcp

import (
	"github.com/phildougherty/m8e/internal/ai"
	"github.com/phildougherty/m8e/internal/compose"
	"github.com/phildougherty/m8e/internal/memory"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// MateyMCPServer provides MCP tools for interacting with Matey and the cluster
type MateyMCPServer struct {
	mateyBinary      string
	configFile       string
	namespace        string
	k8sClient        client.Client
	clientset        kubernetes.Interface
	composer         *compose.K8sComposer
	config           *rest.Config
	memoryStore      *memory.MemoryStore
	memoryTools      *memory.MCPMemoryTools
	workspaceManager *WorkspaceManager
	agentFactory     AgentFactory // Factory function for creating recursive TermChat instances
	aiManager        *ai.Manager  // AI provider for real LLM reasoning
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

