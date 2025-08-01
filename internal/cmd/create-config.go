// internal/cmd/create-config.go
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/constants"

	"github.com/spf13/cobra"
)

func NewCreateConfigCommand() *cobra.Command {
	var outputDir string
	var clientType string
	cmd := &cobra.Command{
		Use:   "create-config",
		Short: "Create client configuration for MCP servers",
		Long: `Generate ready-to-use configuration files for MCP servers that can be
imported directly into LLM clients like Claude Desktop, Anthropic API clients,
or OpenAI compatible clients.
This makes it easy to use your MCP servers with popular LLM client applications.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			file, _ := cmd.Flags().GetString("file")
			// Create output directory if it doesn't exist
			if outputDir == "" {
				outputDir = "client-configs"
			}
			if err := os.MkdirAll(outputDir, constants.DefaultDirMode); err != nil {

				return fmt.Errorf("failed to create output directory: %w", err)
			}
			// Load the MCP compose configuration
			cfg, err := config.LoadConfig(file)
			if err != nil {

				return fmt.Errorf("failed to load config: %w", err)
			}
			// Generate client configuration based on type
			switch strings.ToLower(clientType) {
			case "claude":

				return generateClaudeConfig(cfg, outputDir)
			case "claude-code":

				return generateClaudeCodeConfig(cfg, outputDir)
			case "gemini":

				return generateGeminiConfig(cfg, outputDir)
			case "anthropic":

				return generateAnthropicConfig(cfg, outputDir)
			case "openai":

				return generateOpenAIConfig(cfg, outputDir)
			case "opencode":

				return generateOpenCodeConfig(cfg, outputDir)
			case "all":
				if err := generateClaudeConfig(cfg, outputDir); err != nil {

					return err
				}
				if err := generateClaudeCodeConfig(cfg, outputDir); err != nil {

					return err
				}
				if err := generateGeminiConfig(cfg, outputDir); err != nil {

					return err
				}
				if err := generateAnthropicConfig(cfg, outputDir); err != nil {

					return err
				}
				if err := generateOpenAIConfig(cfg, outputDir); err != nil {

					return err
				}

				return generateOpenCodeConfig(cfg, outputDir)
			default:

				return fmt.Errorf("unknown client type: %s", clientType)
			}
		},
	}
	// Use different flag names to avoid conflict with the global -c flag
	cmd.Flags().StringVarP(&outputDir, "output", "o", "client-configs", "Directory to output client configurations")
	cmd.Flags().StringVarP(&clientType, "type", "t", "all", "Client type (claude, claude-code, gemini, anthropic, openai, opencode, all)")

	return cmd
}

// generateClaudeConfig creates configurations for Claude Desktop
func generateClaudeConfig(cfg *config.ComposeConfig, outputDir string) error {
	fmt.Println("Generating Claude Desktop configuration...")

	// Claude Desktop uses a JSON format for importing servers
	type claudeServer struct {
		Name         string   `json:"name"`
		Command      string   `json:"command,omitempty"`
		Args         []string `json:"args,omitempty"`
		Capabilities []string `json:"capabilities"`
		Description  string   `json:"description,omitempty"`
	}

	servers := make([]claudeServer, 0, len(cfg.Servers))

	for name, srvCfg := range cfg.Servers {
		server := claudeServer{
			Name:         name,
			Capabilities: srvCfg.Capabilities,
			Description:  fmt.Sprintf("MCP server for %s", name),
		}

		// If it's a process-based server, use the command directly
		if srvCfg.Command != "" {
			server.Command = srvCfg.Command
			server.Args = srvCfg.Args
		} else if srvCfg.Image != "" {
			// For container-based servers, create a wrapper command
			// Claude Desktop can't run Docker commands directly, so we create a script
			scriptName := fmt.Sprintf("run-%s.sh", name)
			scriptPath := filepath.Join(outputDir, scriptName)

			script := fmt.Sprintf(`#!/bin/bash
# Wrapper script for running %s in Docker
docker run --rm -i \
`, name)

			// Add environment variables
			for k, v := range srvCfg.Env {
				script += fmt.Sprintf("  -e %s=%s \\\n", k, v)
			}

			// Add volumes if any
			for _, v := range srvCfg.Volumes {
				script += fmt.Sprintf("  -v %s \\\n", v)
			}

			// Add the image and command
			script += fmt.Sprintf("  %s", srvCfg.Image)
			if srvCfg.Command != "" {
				script += fmt.Sprintf(" %s", srvCfg.Command)
				if len(srvCfg.Args) > 0 {
					script += fmt.Sprintf(" %s", strings.Join(srvCfg.Args, " "))
				}
			}

			// Write the script
			if err := os.WriteFile(scriptPath, []byte(script), constants.ExecutableFileMode); err != nil {

				return fmt.Errorf("failed to write script file: %w", err)
			}

			// Use the script as the command
			server.Command = scriptPath
			server.Args = []string{}
		}

		servers = append(servers, server)
	}

	// Create the Claude Desktop config file
	configPath := filepath.Join(outputDir, "claude-desktop-servers.json")
	configData, err := json.MarshalIndent(servers, "", "  ")
	if err != nil {

		return fmt.Errorf("failed to marshal Claude Desktop config: %w", err)
	}

	if err := os.WriteFile(configPath, configData, constants.DefaultFileMode); err != nil {

		return fmt.Errorf("failed to write Claude Desktop config file: %w", err)
	}

	fmt.Printf("Claude Desktop configuration created at %s\n", configPath)
	fmt.Println("To use with Claude Desktop:")
	fmt.Println("1. Open Claude Desktop")
	fmt.Println("2. Go to Settings > MCP Servers")
	fmt.Println("3. Click 'Import Servers' and select the generated file")

	return nil
}

// generateClaudeCodeConfig creates .mcp.json configuration for Claude Code
func generateClaudeCodeConfig(cfg *config.ComposeConfig, outputDir string) error {
	fmt.Println("Generating Claude Code .mcp.json configuration...")

	// Claude Code .mcp.json structure
	type mcpOAuthConfig struct {
		DiscoveryURL string `json:"discoveryUrl"`
	}

	type mcpServer struct {
		Type      string                 `json:"type"`
		URL       string                 `json:"url"`
		Headers   map[string]string      `json:"headers,omitempty"`
		OAuth     *mcpOAuthConfig        `json:"oauth,omitempty"`
	}

	type mcpConfig struct {
		McpServers map[string]mcpServer `json:"mcpServers"`
	}

	config := mcpConfig{
		McpServers: make(map[string]mcpServer),
	}

	// Get proxy URL and API key  
	proxyURL := cfg.GetProxyURL()
	// Remove protocol prefix to get just the host
	proxyHost := strings.TrimPrefix(proxyURL, "https://")
	proxyHost = strings.TrimPrefix(proxyHost, "http://")
	// Remove port if present
	if idx := strings.Index(proxyHost, ":"); idx != -1 {
		proxyHost = proxyHost[:idx]
	}

	apiKey := cfg.ProxyAuth.APIKey
	if apiKey == "" {
		apiKey = "myapikey" // fallback default
	}

	// Process each server
	for name, srvCfg := range cfg.Servers {
		// Determine the protocol
		protocol := srvCfg.Protocol
		if protocol == "" {
			protocol = "http" // default
		}

		server := mcpServer{
			Type: protocol, // Use the actual server protocol
		}

		// Build the URL using proxy host
		if proxyHost != "" {
			server.URL = fmt.Sprintf("https://%s/%s", proxyHost, name)
		} else {
			// Fallback to direct server URL if no proxy host
			if srvCfg.HttpPort > 0 {
				server.URL = fmt.Sprintf("http://localhost:%d", srvCfg.HttpPort)
			} else {
				server.URL = "http://localhost:8080" // default port
			}
		}

		// Add OAuth discovery if OAuth is enabled in config
		if cfg.OAuth != nil && cfg.OAuth.Enabled {
			server.OAuth = &mcpOAuthConfig{
				DiscoveryURL: fmt.Sprintf("https://%s/.well-known/oauth-authorization-server/%s", proxyHost, name),
			}
		} else if apiKey != "" {
			// Fall back to API key authentication if OAuth is not enabled
			server.Headers = map[string]string{
				"Authorization": fmt.Sprintf("Bearer %s", apiKey),
			}
		}

		config.McpServers[name] = server
	}

	// Write the .mcp.json file
	configPath := filepath.Join(outputDir, ".mcp.json")
	configData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal Claude Code config: %w", err)
	}

	if err := os.WriteFile(configPath, configData, constants.DefaultFileMode); err != nil {
		return fmt.Errorf("failed to write Claude Code config file: %w", err)
	}

	fmt.Printf("Claude Code configuration created at %s\n", configPath)
	fmt.Println("To use with Claude Code:")
	fmt.Println("1. Copy the .mcp.json file to your project directory")
	fmt.Println("2. Run 'claude-code' in the directory containing .mcp.json")
	fmt.Println("3. The MCP servers will be automatically loaded")

	return nil
}

// generateGeminiConfig creates .gemini/settings.json configuration for Gemini CLI
func generateGeminiConfig(cfg *config.ComposeConfig, outputDir string) error {
	fmt.Println("Generating Gemini CLI configuration...")

	// Gemini CLI settings.json structure - Note: Gemini CLI does not support OAuth yet
	type geminiServer struct {
		URL     string            `json:"url,omitempty"`
		HttpURL string            `json:"httpUrl,omitempty"`
		Headers map[string]string `json:"headers,omitempty"`
		Env     map[string]string `json:"env,omitempty"`
		Timeout int               `json:"timeout,omitempty"`
		Trust   bool              `json:"trust,omitempty"`
	}

	type geminiConfig struct {
		McpServers map[string]geminiServer `json:"mcpServers"`
	}

	config := geminiConfig{
		McpServers: make(map[string]geminiServer),
	}

	// Get proxy URL and API key  
	proxyURL := cfg.GetProxyURL()
	// Remove protocol prefix to get just the host
	proxyHost := strings.TrimPrefix(proxyURL, "https://")
	proxyHost = strings.TrimPrefix(proxyHost, "http://")
	// Remove port if present
	if idx := strings.Index(proxyHost, ":"); idx != -1 {
		proxyHost = proxyHost[:idx]
	}

	apiKey := cfg.ProxyAuth.APIKey
	if apiKey == "" {
		apiKey = "myapikey" // fallback default
	}

	// Process each server
	for name, srvCfg := range cfg.Servers {
		server := geminiServer{
			Timeout: 30000, // 30 second timeout
			Trust:   false, // Default to not trusted
		}

		// Build the URL based on protocol
		protocol := srvCfg.Protocol
		if protocol == "" {
			protocol = "http" // default
		}

		serverURL := fmt.Sprintf("https://%s/%s", proxyHost, name)

		if protocol == "sse" {
			server.URL = serverURL // Use url for SSE (expects text/event-stream)
		} else {
			server.HttpURL = serverURL // Use httpUrl for HTTP (supports streamable responses)
		}

		// Gemini CLI only supports Bearer token authentication via headers
		// OAuth is not supported yet, so always use API key if available
		if apiKey != "" {
			server.Headers = map[string]string{
				"Authorization": fmt.Sprintf("Bearer %s", apiKey),
			}
		}

		// Add environment variables if any
		if len(srvCfg.Env) > 0 {
			server.Env = make(map[string]string)
			for k, v := range srvCfg.Env {
				server.Env[k] = v
			}
		}

		config.McpServers[name] = server
	}

	// Add the matey MCP server (internal cluster management tools)
	mateyServer := geminiServer{
		HttpURL: fmt.Sprintf("https://%s/matey", proxyHost),
		Timeout: 30000,
		Trust:   false,
	}
	
	// Matey server typically allows API key auth, so use that if available
	if apiKey != "" {
		mateyServer.Headers = map[string]string{
			"Authorization": fmt.Sprintf("Bearer %s", apiKey),
		}
	}
	
	config.McpServers["matey"] = mateyServer

	// Create .gemini directory in output directory
	geminiDir := filepath.Join(outputDir, ".gemini")
	if err := os.MkdirAll(geminiDir, constants.DefaultDirMode); err != nil {
		return fmt.Errorf("failed to create .gemini directory: %w", err)
	}

	// Write the settings.json file
	configPath := filepath.Join(geminiDir, "settings.json")
	configData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal Gemini config: %w", err)
	}

	if err := os.WriteFile(configPath, configData, constants.DefaultFileMode); err != nil {
		return fmt.Errorf("failed to write Gemini config file: %w", err)
	}

	fmt.Printf("Gemini CLI configuration created at %s\n", configPath)
	fmt.Println("To use with Gemini CLI:")
	fmt.Println("1. Copy the .gemini directory to your project root")
	fmt.Println("2. Run 'gemini' in the directory containing .gemini/settings.json")
	fmt.Println("3. The MCP servers will be automatically loaded")
	fmt.Println("4. Alternatively, copy to ~/.gemini/settings.json for global config")

	return nil
}

// generateAnthropicConfig creates configurations for Anthropic API clients
func generateAnthropicConfig(cfg *config.ComposeConfig, outputDir string) error {
	fmt.Println("Generating Anthropic API client configuration...")

	// Create a Python script that demonstrates how to use the servers with Anthropic API
	pythonCode := `"""
Example script for using MCP servers with Anthropic API
"""
import os
import subprocess
import json
from anthropic import Anthropic
# Initialize Anthropic client
client = Anthropic(api_key=os.environ.get("ANTHROPIC_API_KEY"))
# Define MCP servers
MCP_SERVERS = {
`

	for name, srvCfg := range cfg.Servers {
		pythonCode += fmt.Sprintf(`    "%s": {
        "capabilities": %s,
`, name, formatStrListPython(srvCfg.Capabilities))

		if srvCfg.Command != "" {
			pythonCode += fmt.Sprintf(`        "command": "%s",
        "args": %s,
`, srvCfg.Command, formatStrListPython(srvCfg.Args))
		}

		if srvCfg.Image != "" {
			pythonCode += fmt.Sprintf(`        "image": "%s",
`, srvCfg.Image)
		}

		// Remove trailing comma on the last line
		pythonCode = strings.TrimSuffix(pythonCode, ",\n") + "\n"
		pythonCode += `    },
`
	}

	pythonCode += `}
def start_mcp_server(server_name):
    """Start an MCP server and return the process"""
    server_config = MCP_SERVERS.get(server_name)
    if not server_config:
        raise ValueError(f"Unknown server: {server_name}")
    
    if "command" in server_config:
        # Process-based server

        return subprocess.Popen(
            [server_config["command"]] + server_config.get("args", []),
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True
        )
    elif "image" in server_config:
        # Container-based server
        cmd = ["docker", "run", "--rm", "-i"]
        if server_config.get("command"):
            cmd.extend([server_config["image"], server_config["command"]])
            if server_config.get("args"):
                cmd.extend(server_config["args"])
        else:
            cmd.append(server_config["image"])
        

        return subprocess.Popen(
            cmd,
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True
        )
# Example: Using a server with Claude
def query_claude_with_mcp(prompt, server_name=None):
    """Query Claude with optional MCP server integration"""
    message_params = {
        "model": "claude-3-opus-20240229",
        "max_tokens": 1000,
        "messages": [
            {"role": "user", "content": prompt}
        ],
    }
    
    if server_name:
        # Add MCP server info
        server_process = start_mcp_server(server_name)
        
        # In a real implementation, you would need to handle the MCP protocol
        # communication between the Anthropic API and the server process
        
        # This is a simplified example
        message_params["mcp_servers"] = [{
            "name": server_name,
            "capabilities": MCP_SERVERS[server_name]["capabilities"]
        }]
    
    response = client.messages.create(**message_params)

    return response.content[0].text
# Example usage
if __name__ == "__main__":
    # Use Claude without MCP
    response = query_claude_with_mcp("What is the capital of France?")
    print("Response without MCP:", response)
    
    # Use Claude with MCP server
    # Replace with your server name
    server_name = list(MCP_SERVERS.keys())[0]
    response = query_claude_with_mcp(
        f"Use the {server_name} server to help me with this task...",
        server_name=server_name
    )
    print(f"Response with {server_name} server:", response)
`

	// Write the Python script
	pythonPath := filepath.Join(outputDir, "anthropic_mcp_example.py")
	if err := os.WriteFile(pythonPath, []byte(pythonCode), constants.DefaultFileMode); err != nil {

		return fmt.Errorf("failed to write Anthropic example script: %w", err)
	}

	fmt.Printf("Anthropic API example created at %s\n", pythonPath)
	fmt.Println("To use with Anthropic API:")
	fmt.Println("1. Install the Anthropic Python client: pip install anthropic")
	fmt.Println("2. Set your ANTHROPIC_API_KEY environment variable")
	fmt.Println("3. Run the example script: python anthropic_mcp_example.py")

	return nil
}

// generateOpenAIConfig creates configurations for OpenAI compatible clients
func generateOpenAIConfig(cfg *config.ComposeConfig, outputDir string) error {
	fmt.Println("Generating OpenAI compatible client configuration...")

	// Create a Node.js script that demonstrates how to use the servers with OpenAI API
	jsCode := `/**
 * Example script for using MCP servers with OpenAI API
 */
const { spawn } = require('child_process');
const { OpenAI } = require('openai');
// Initialize OpenAI client
const openai = new OpenAI({
  apiKey: process.env.OPENAI_API_KEY,
});
// Define MCP servers
const MCP_SERVERS = {
`

	for name, srvCfg := range cfg.Servers {
		jsCode += fmt.Sprintf(`  '%s': {
    capabilities: %s,
`, name, formatStrListJS(srvCfg.Capabilities))

		if srvCfg.Command != "" {
			jsCode += fmt.Sprintf(`    command: '%s',
    args: %s,
`, srvCfg.Command, formatStrListJS(srvCfg.Args))
		}

		if srvCfg.Image != "" {
			jsCode += fmt.Sprintf(`    image: '%s',
`, srvCfg.Image)
		}

		// Remove trailing comma on the last line
		jsCode = strings.TrimSuffix(jsCode, ",\n") + "\n"
		jsCode += `  },
`
	}

	jsCode += `};
/**
 * Start an MCP server and return the process
 */
function startMcpServer(serverName) {
  const serverConfig = MCP_SERVERS[serverName];
  if (!serverConfig) {
    throw new Error("Unknown server: " + serverName);
  }
  
  if (serverConfig.command) {
    // Process-based server

    return spawn(
      serverConfig.command,
      serverConfig.args || [],
      { stdio: ['pipe', 'pipe', 'pipe'] }
    );
  } else if (serverConfig.image) {
    // Container-based server
    const cmd = ['docker', 'run', '--rm', '-i'];
    if (serverConfig.command) {
      cmd.push(serverConfig.image, serverConfig.command);
      if (serverConfig.args && serverConfig.args.length > 0) {
        cmd.push(...serverConfig.args);
      }
    } else {
      cmd.push(serverConfig.image);
    }
    

    return spawn('docker', cmd, { stdio: ['pipe', 'pipe', 'pipe'] });
  }
}
/**
 * Query OpenAI with optional MCP server integration
 */
async function queryOpenAIWithMCP(prompt, serverName = null) {
  const messageParams = {
    model: 'gpt-4',
    max_tokens: 1000,
    messages: [
      { role: 'user', content: prompt }
    ],
  };
  
  if (serverName) {
    // Add MCP server info
    const serverProcess = startMcpServer(serverName);
    
    // In a real implementation, you would need to handle the MCP protocol
    // communication between the OpenAI API and the server process
    
    // This is a simplified example
    messageParams.tools = [{
      type: 'mcp_server',
      mcp_server: {
        name: serverName,
        capabilities: MCP_SERVERS[serverName].capabilities
      }
    }];
  }
  
  const response = await openai.chat.completions.create(messageParams);

  return response.choices[0].message.content;
}
// Example usage
async function main() {
  try {
    // Use OpenAI without MCP
    const responseWithoutMCP = await queryOpenAIWithMCP('What is the capital of France?');
    console.log('Response without MCP:', responseWithoutMCP);
    
    // Use OpenAI with MCP server
    // Replace with your server name
    const serverName = Object.keys(MCP_SERVERS)[0];
    const responseWithMCP = await queryOpenAIWithMCP(
      "Use the " + serverName + " server to help me with this task...",
      serverName
    );
    console.log("Response with " + serverName + " server:", responseWithMCP);
  } catch (error) {
    console.error('Error:', error);
  }
}
main();
`

	// Write the JS script
	jsPath := filepath.Join(outputDir, "openai_mcp_example.js")
	if err := os.WriteFile(jsPath, []byte(jsCode), constants.DefaultFileMode); err != nil {

		return fmt.Errorf("failed to write OpenAI example script: %w", err)
	}

	// Create a package.json file
	packageJSON := `{
  "name": "openai-mcp-example",
  "version": "1.0.0",
  "description": "Example of using MCP servers with OpenAI API",
  "main": "openai_mcp_example.js",
  "dependencies": {
    "openai": "^4.0.0"
  },
  "scripts": {
    "start": "node openai_mcp_example.js"
  }
}
`

	packagePath := filepath.Join(outputDir, "package.json")
	if err := os.WriteFile(packagePath, []byte(packageJSON), constants.DefaultFileMode); err != nil {

		return fmt.Errorf("failed to write package.json file: %w", err)
	}

	fmt.Printf("OpenAI API example created at %s\n", jsPath)
	fmt.Println("To use with OpenAI API:")
	fmt.Println("1. Install dependencies: npm install")
	fmt.Println("2. Set your OPENAI_API_KEY environment variable")
	fmt.Println("3. Run the example script: npm start")

	return nil
}

// formatStrListPython formats a slice of strings as a Python list
func formatStrListPython(strs []string) string {
	items := make([]string, len(strs))
	for i, s := range strs {
		items[i] = fmt.Sprintf(`"%s"`, s)
	}

	return "[" + strings.Join(items, ", ") + "]"
}

// formatStrListJS formats a slice of strings as a JavaScript array
func formatStrListJS(strs []string) string {
	items := make([]string, len(strs))
	for i, s := range strs {
		items[i] = fmt.Sprintf(`'%s'`, s)
	}

	return "[" + strings.Join(items, ", ") + "]"
}

// generateOpenCodeConfig creates .opencode.json configuration for OpenCode TUI
func generateOpenCodeConfig(cfg *config.ComposeConfig, outputDir string) error {
	fmt.Println("Generating OpenCode TUI configuration...")

	// OpenCode .opencode.json structure
	type openCodeServer struct {
		Type    string            `json:"type"`
		Command string            `json:"command,omitempty"`
		Args    []string          `json:"args,omitempty"`
		URL     string            `json:"url,omitempty"`
		Headers map[string]string `json:"headers,omitempty"`
	}

	type openCodeConfig struct {
		McpServers map[string]openCodeServer `json:"mcpServers"`
		Providers  map[string]interface{}    `json:"providers,omitempty"`
		Shell      map[string]interface{}    `json:"shell,omitempty"`
	}

	config := openCodeConfig{
		McpServers: make(map[string]openCodeServer),
		Providers: map[string]interface{}{
			"openai": map[string]interface{}{
				"disabled": false,
			},
			"anthropic": map[string]interface{}{
				"disabled": false,
			},
		},
		Shell: map[string]interface{}{
			"path": "/bin/bash",
			"args": []string{"-l"},
		},
	}

	// Get proxy URL and API key  
	proxyURL := cfg.GetProxyURL()
	// Remove protocol prefix to get just the host
	proxyHost := strings.TrimPrefix(proxyURL, "https://")
	proxyHost = strings.TrimPrefix(proxyHost, "http://")
	// Remove port if present
	if idx := strings.Index(proxyHost, ":"); idx != -1 {
		proxyHost = proxyHost[:idx]
	}

	apiKey := cfg.ProxyAuth.APIKey
	if apiKey == "" {
		apiKey = "myapikey" // fallback default
	}

	// Process each server
	for name, srvCfg := range cfg.Servers {
		server := openCodeServer{}

		// Determine server type and configuration
		if srvCfg.Command != "" {
			// Process-based server (stdio)
			server.Type = "stdio"
			server.Command = srvCfg.Command
			server.Args = srvCfg.Args
		} else if srvCfg.Image != "" {
			// Container-based server - use HTTP proxy
			protocol := srvCfg.Protocol
			if protocol == "" {
				protocol = "http"
			}

			if protocol == "sse" {
				server.Type = "sse"
			} else {
				server.Type = "http"
			}

			// Build the URL through the proxy
			server.URL = fmt.Sprintf("https://%s/%s", proxyHost, name)

			// Add authentication headers if API key is provided
			if apiKey != "" {
				server.Headers = map[string]string{
					"Authorization": fmt.Sprintf("Bearer %s", apiKey),
				}
			}
		} else {
			// Default to HTTP if no specific configuration
			server.Type = "http"
			server.URL = fmt.Sprintf("https://%s/%s", proxyHost, name)

			if apiKey != "" {
				server.Headers = map[string]string{
					"Authorization": fmt.Sprintf("Bearer %s", apiKey),
				}
			}
		}

		config.McpServers[name] = server
	}

	// Add the matey MCP server (internal cluster management tools)
	mateyServer := openCodeServer{
		Type: "http",
		URL:  fmt.Sprintf("https://%s/matey", proxyHost),
	}

	// Add authentication headers if API key is provided
	if apiKey != "" {
		mateyServer.Headers = map[string]string{
			"Authorization": fmt.Sprintf("Bearer %s", apiKey),
		}
	}

	config.McpServers["matey"] = mateyServer

	// Write the .opencode.json file
	configPath := filepath.Join(outputDir, ".opencode.json")
	configData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal OpenCode config: %w", err)
	}

	if err := os.WriteFile(configPath, configData, constants.DefaultFileMode); err != nil {
		return fmt.Errorf("failed to write OpenCode config file: %w", err)
	}

	fmt.Printf("OpenCode TUI configuration created at %s\n", configPath)
	fmt.Println("To use with OpenCode TUI:")
	fmt.Println("1. Copy the .opencode.json file to your project directory")
	fmt.Println("2. Set your ANTHROPIC_API_KEY or OPENAI_API_KEY environment variables")
	fmt.Println("3. Run 'opencode' in the directory containing .opencode.json")
	fmt.Println("4. The MCP servers will be automatically loaded")
	fmt.Println("5. Alternatively, copy to ~/.opencode.json for global config")

	return nil
}
