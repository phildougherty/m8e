// internal/service/config_generator.go
package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/constants"
)

// ProxyAPIKeyPlaceholder is written into generated client configs when no real
// key is available. It is deliberately loud: a previous "myapikey" fallback
// silently produced configs that looked valid but never authenticated.
const ProxyAPIKeyPlaceholder = "REPLACE_WITH_YOUR_MCP_API_KEY"

// ConfigGenerator generates ready-to-use MCP client configuration files for
// the servers in a ComposeConfig. It writes files to an output directory and
// holds no cobra/stdout concerns: progress is reported through a Reporter and
// the placeholder-key warning through a separate warn callback.
type ConfigGenerator struct {
	cfg       *config.ComposeConfig
	outputDir string
	// warn receives the no-API-key warning. The CLI routes it to stderr.
	warn func(msg string)
}

// NewConfigGenerator builds a generator for cfg writing into outputDir. warn
// may be nil, in which case the placeholder-key warning is suppressed.
func NewConfigGenerator(cfg *config.ComposeConfig, outputDir string, warn func(string)) *ConfigGenerator {
	if warn == nil {
		warn = func(string) {}
	}

	return &ConfigGenerator{cfg: cfg, outputDir: outputDir, warn: warn}
}

// resolveProxyAPIKey picks the proxy API key to embed in a generated client
// config: matey.yaml's value if set, else $MCP_API_KEY, else a loud
// placeholder plus a warning.
func (g *ConfigGenerator) resolveProxyAPIKey() string {
	if g.cfg.ProxyAuth.APIKey != "" {
		return g.cfg.ProxyAuth.APIKey
	}
	if env := os.Getenv("MCP_API_KEY"); env != "" {
		return env
	}
	g.warn(fmt.Sprintf(
		"warning: no proxy API key found (proxy_auth.api_key in matey.yaml or $MCP_API_KEY); "+
			"generated config contains the placeholder %q which you must replace",
		ProxyAPIKeyPlaceholder))

	return ProxyAPIKeyPlaceholder
}

// proxyHost returns the proxy hostname stripped of scheme and port.
func (g *ConfigGenerator) proxyHost() string {
	proxyHost := strings.TrimPrefix(g.cfg.GetProxyURL(), "https://")
	proxyHost = strings.TrimPrefix(proxyHost, "http://")
	if idx := strings.Index(proxyHost, ":"); idx != -1 {
		proxyHost = proxyHost[:idx]
	}

	return proxyHost
}

// Generate produces the config(s) for the given client type. clientType "all"
// generates every supported client. Each written file or instruction line is
// reported through report (which may be nil).
func (g *ConfigGenerator) Generate(clientType string, report Reporter) error {
	if report == nil {
		report = func(string) {}
	}

	switch strings.ToLower(clientType) {
	case "claude":
		return g.generateClaude(report)
	case "claude-code":
		return g.generateClaudeCode(report)
	case "gemini":
		return g.generateGemini(report)
	case "anthropic":
		return g.generateAnthropic(report)
	case "openai":
		return g.generateOpenAI(report)
	case "opencode":
		return g.generateOpenCode(report)
	case "all":
		for _, gen := range []func(Reporter) error{
			g.generateClaude,
			g.generateClaudeCode,
			g.generateGemini,
			g.generateAnthropic,
			g.generateOpenAI,
			g.generateOpenCode,
		} {
			if err := gen(report); err != nil {
				return err
			}
		}

		return nil
	default:
		return fmt.Errorf("unknown client type: %s", clientType)
	}
}

func (g *ConfigGenerator) generateClaude(report Reporter) error {
	report("Generating Claude Desktop configuration...")

	type claudeServer struct {
		Name         string   `json:"name"`
		Command      string   `json:"command,omitempty"`
		Args         []string `json:"args,omitempty"`
		Capabilities []string `json:"capabilities"`
		Description  string   `json:"description,omitempty"`
	}

	servers := make([]claudeServer, 0, len(g.cfg.Servers))

	for name, srvCfg := range g.cfg.Servers {
		server := claudeServer{
			Name:         name,
			Capabilities: srvCfg.Capabilities,
			Description:  fmt.Sprintf("MCP server for %s", name),
		}

		if srvCfg.Command != "" {
			server.Command = srvCfg.Command
			server.Args = srvCfg.Args
		} else if srvCfg.Image != "" {
			// Claude Desktop cannot run Docker directly, so emit a wrapper script.
			scriptName := fmt.Sprintf("run-%s.sh", name)
			scriptPath := filepath.Join(g.outputDir, scriptName)

			script := fmt.Sprintf(`#!/bin/bash
# Wrapper script for running %s in Docker
docker run --rm -i \
`, name)

			for k, v := range srvCfg.Env {
				script += fmt.Sprintf("  -e %s=%s \\\n", k, v)
			}
			for _, v := range srvCfg.Volumes {
				script += fmt.Sprintf("  -v %s \\\n", v)
			}

			script += fmt.Sprintf("  %s", srvCfg.Image)
			if srvCfg.Command != "" {
				script += fmt.Sprintf(" %s", srvCfg.Command)
				if len(srvCfg.Args) > 0 {
					script += fmt.Sprintf(" %s", strings.Join(srvCfg.Args, " "))
				}
			}

			if err := os.WriteFile(scriptPath, []byte(script), constants.ExecutableFileMode); err != nil {
				return fmt.Errorf("failed to write script file: %w", err)
			}

			server.Command = scriptPath
			server.Args = []string{}
		}

		servers = append(servers, server)
	}

	configPath := filepath.Join(g.outputDir, "claude-desktop-servers.json")
	configData, err := json.MarshalIndent(servers, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal Claude Desktop config: %w", err)
	}

	if err := os.WriteFile(configPath, configData, constants.DefaultFileMode); err != nil {
		return fmt.Errorf("failed to write Claude Desktop config file: %w", err)
	}

	report(fmt.Sprintf("Claude Desktop configuration created at %s", configPath))
	report("To use with Claude Desktop:")
	report("1. Open Claude Desktop")
	report("2. Go to Settings > MCP Servers")
	report("3. Click 'Import Servers' and select the generated file")

	return nil
}

func (g *ConfigGenerator) generateClaudeCode(report Reporter) error {
	report("Generating Claude Code .mcp.json configuration...")

	type mcpOAuthConfig struct {
		DiscoveryURL string `json:"discoveryUrl"`
	}

	type mcpServer struct {
		Type    string            `json:"type"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers,omitempty"`
		OAuth   *mcpOAuthConfig   `json:"oauth,omitempty"`
	}

	type mcpConfig struct {
		McpServers map[string]mcpServer `json:"mcpServers"`
	}

	cfgOut := mcpConfig{McpServers: make(map[string]mcpServer)}

	proxyHost := g.proxyHost()
	apiKey := g.resolveProxyAPIKey()

	for name, srvCfg := range g.cfg.Servers {
		protocol := srvCfg.Protocol
		if protocol == "" {
			protocol = "http"
		}

		server := mcpServer{Type: protocol}

		if proxyHost != "" {
			server.URL = fmt.Sprintf("https://%s/%s", proxyHost, name)
		} else {
			if srvCfg.HttpPort > 0 {
				server.URL = fmt.Sprintf("http://localhost:%d", srvCfg.HttpPort)
			} else {
				server.URL = "http://localhost:8080"
			}
		}

		if g.cfg.OAuth != nil && g.cfg.OAuth.Enabled {
			server.OAuth = &mcpOAuthConfig{
				DiscoveryURL: fmt.Sprintf("https://%s/.well-known/oauth-authorization-server/%s", proxyHost, name),
			}
		} else if apiKey != "" {
			server.Headers = map[string]string{
				"Authorization": fmt.Sprintf("Bearer %s", apiKey),
			}
		}

		cfgOut.McpServers[name] = server
	}

	configPath := filepath.Join(g.outputDir, ".mcp.json")
	configData, err := json.MarshalIndent(cfgOut, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal Claude Code config: %w", err)
	}

	if err := os.WriteFile(configPath, configData, constants.DefaultFileMode); err != nil {
		return fmt.Errorf("failed to write Claude Code config file: %w", err)
	}

	report(fmt.Sprintf("Claude Code configuration created at %s", configPath))
	report("To use with Claude Code:")
	report("1. Copy the .mcp.json file to your project directory")
	report("2. Run 'claude-code' in the directory containing .mcp.json")
	report("3. The MCP servers will be automatically loaded")

	return nil
}

func (g *ConfigGenerator) generateGemini(report Reporter) error {
	report("Generating Gemini CLI configuration...")

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

	cfgOut := geminiConfig{McpServers: make(map[string]geminiServer)}

	proxyHost := g.proxyHost()
	apiKey := g.resolveProxyAPIKey()

	for name, srvCfg := range g.cfg.Servers {
		server := geminiServer{
			Timeout: 30000,
			Trust:   false,
		}

		protocol := srvCfg.Protocol
		if protocol == "" {
			protocol = "http"
		}

		serverURL := fmt.Sprintf("https://%s/%s", proxyHost, name)

		if protocol == "sse" {
			server.URL = serverURL
		} else {
			server.HttpURL = serverURL
		}

		if apiKey != "" {
			server.Headers = map[string]string{
				"Authorization": fmt.Sprintf("Bearer %s", apiKey),
			}
		}

		if len(srvCfg.Env) > 0 {
			server.Env = make(map[string]string)
			for k, v := range srvCfg.Env {
				server.Env[k] = v
			}
		}

		cfgOut.McpServers[name] = server
	}

	mateyServer := geminiServer{
		HttpURL: fmt.Sprintf("https://%s/matey", proxyHost),
		Timeout: 30000,
		Trust:   false,
	}
	if apiKey != "" {
		mateyServer.Headers = map[string]string{
			"Authorization": fmt.Sprintf("Bearer %s", apiKey),
		}
	}
	cfgOut.McpServers["matey"] = mateyServer

	geminiDir := filepath.Join(g.outputDir, ".gemini")
	if err := os.MkdirAll(geminiDir, constants.DefaultDirMode); err != nil {
		return fmt.Errorf("failed to create .gemini directory: %w", err)
	}

	configPath := filepath.Join(geminiDir, "settings.json")
	configData, err := json.MarshalIndent(cfgOut, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal Gemini config: %w", err)
	}

	if err := os.WriteFile(configPath, configData, constants.DefaultFileMode); err != nil {
		return fmt.Errorf("failed to write Gemini config file: %w", err)
	}

	report(fmt.Sprintf("Gemini CLI configuration created at %s", configPath))
	report("To use with Gemini CLI:")
	report("1. Copy the .gemini directory to your project root")
	report("2. Run 'gemini' in the directory containing .gemini/settings.json")
	report("3. The MCP servers will be automatically loaded")
	report("4. Alternatively, copy to ~/.gemini/settings.json for global config")

	return nil
}

func (g *ConfigGenerator) generateAnthropic(report Reporter) error {
	report("Generating Anthropic API client configuration...")

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

	for name, srvCfg := range g.cfg.Servers {
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

	pythonPath := filepath.Join(g.outputDir, "anthropic_mcp_example.py")
	if err := os.WriteFile(pythonPath, []byte(pythonCode), constants.DefaultFileMode); err != nil {
		return fmt.Errorf("failed to write Anthropic example script: %w", err)
	}

	report(fmt.Sprintf("Anthropic API example created at %s", pythonPath))
	report("To use with Anthropic API:")
	report("1. Install the Anthropic Python client: pip install anthropic")
	report("2. Set your ANTHROPIC_API_KEY environment variable")
	report("3. Run the example script: python anthropic_mcp_example.py")

	return nil
}

func (g *ConfigGenerator) generateOpenAI(report Reporter) error {
	report("Generating OpenAI compatible client configuration...")

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

	for name, srvCfg := range g.cfg.Servers {
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

	jsPath := filepath.Join(g.outputDir, "openai_mcp_example.js")
	if err := os.WriteFile(jsPath, []byte(jsCode), constants.DefaultFileMode); err != nil {
		return fmt.Errorf("failed to write OpenAI example script: %w", err)
	}

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

	packagePath := filepath.Join(g.outputDir, "package.json")
	if err := os.WriteFile(packagePath, []byte(packageJSON), constants.DefaultFileMode); err != nil {
		return fmt.Errorf("failed to write package.json file: %w", err)
	}

	report(fmt.Sprintf("OpenAI API example created at %s", jsPath))
	report("To use with OpenAI API:")
	report("1. Install dependencies: npm install")
	report("2. Set your OPENAI_API_KEY environment variable")
	report("3. Run the example script: npm start")

	return nil
}

func (g *ConfigGenerator) generateOpenCode(report Reporter) error {
	report("Generating OpenCode TUI configuration...")

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

	cfgOut := openCodeConfig{
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

	proxyHost := g.proxyHost()
	apiKey := g.resolveProxyAPIKey()

	for name, srvCfg := range g.cfg.Servers {
		server := openCodeServer{}

		if srvCfg.Command != "" {
			server.Type = "stdio"
			server.Command = srvCfg.Command
			server.Args = srvCfg.Args
		} else if srvCfg.Image != "" {
			protocol := srvCfg.Protocol
			if protocol == "" {
				protocol = "http"
			}

			if protocol == "sse" {
				server.Type = "sse"
			} else {
				server.Type = "http"
			}

			server.URL = fmt.Sprintf("https://%s/%s", proxyHost, name)

			if apiKey != "" {
				server.Headers = map[string]string{
					"Authorization": fmt.Sprintf("Bearer %s", apiKey),
				}
			}
		} else {
			server.Type = "http"
			server.URL = fmt.Sprintf("https://%s/%s", proxyHost, name)

			if apiKey != "" {
				server.Headers = map[string]string{
					"Authorization": fmt.Sprintf("Bearer %s", apiKey),
				}
			}
		}

		cfgOut.McpServers[name] = server
	}

	mateyServer := openCodeServer{
		Type: "http",
		URL:  fmt.Sprintf("https://%s/matey", proxyHost),
	}
	if apiKey != "" {
		mateyServer.Headers = map[string]string{
			"Authorization": fmt.Sprintf("Bearer %s", apiKey),
		}
	}
	cfgOut.McpServers["matey"] = mateyServer

	configPath := filepath.Join(g.outputDir, ".opencode.json")
	configData, err := json.MarshalIndent(cfgOut, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal OpenCode config: %w", err)
	}

	if err := os.WriteFile(configPath, configData, constants.DefaultFileMode); err != nil {
		return fmt.Errorf("failed to write OpenCode config file: %w", err)
	}

	report(fmt.Sprintf("OpenCode TUI configuration created at %s", configPath))
	report("To use with OpenCode TUI:")
	report("1. Copy the .opencode.json file to your project directory")
	report("2. Set your ANTHROPIC_API_KEY or OPENAI_API_KEY environment variables")
	report("3. Run 'opencode' in the directory containing .opencode.json")
	report("4. The MCP servers will be automatically loaded")
	report("5. Alternatively, copy to ~/.opencode.json for global config")

	return nil
}

// formatStrListPython formats a slice of strings as a Python list literal.
func formatStrListPython(strs []string) string {
	items := make([]string, len(strs))
	for i, s := range strs {
		items[i] = fmt.Sprintf(`"%s"`, s)
	}

	return "[" + strings.Join(items, ", ") + "]"
}

// formatStrListJS formats a slice of strings as a JavaScript array literal.
func formatStrListJS(strs []string) string {
	items := make([]string, len(strs))
	for i, s := range strs {
		items[i] = fmt.Sprintf(`'%s'`, s)
	}

	return "[" + strings.Join(items, ", ") + "]"
}
