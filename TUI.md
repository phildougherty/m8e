# Matey TUI Development Log

## Overview
This document tracks the development and debugging of the enhanced TUI (Terminal User Interface) for Matey, including AI chat integration, streaming fixes, and troubleshooting.

## Major Features Implemented

### 1. Enhanced Configuration Management Tab
- **Location**: `internal/cmd/dashboard_config.go`
- **Features**:
  - YAML editor with syntax highlighting
  - Tree view of configuration resources
  - Real-time validation using existing validation system
  - Configuration templates for all MCP resource types
- **Integration**: Uses `NewConfigValidator()` from existing validation system
- **Templates**: MCPServer, MCPMemory, MCPTaskScheduler, MCPProxy, Workflow, MCPToolbox

### 2. Enhanced Workflow Management Tab
- **Location**: `internal/cmd/dashboard_workflows.go`
- **Features**:
  - Workflow list with status monitoring
  - Template browser (6 built-in templates)
  - Real-time workflow execution monitoring
  - Integration with existing CLI workflow commands
- **Templates**: health-monitoring, data-backup, report-generation, system-maintenance, code-quality-checks, database-maintenance

### 3. AI Configuration Assistant
- **Location**: `internal/cmd/dashboard_ai_config.go`
- **Features**:
  - Context-aware AI prompts with current system state
  - Configuration-specific slash commands
  - Natural language processing for config requests
  - Smart suggestions based on current setup
- **Commands**: `/generate-config`, `/validate-config`, `/suggest-workflow`, `/optimize-config`, `/explain-config`

### 4. Enhanced TUI Interactions
- **Location**: `internal/cmd/dashboard_tui.go`
- **Features**:
  - Tab-based navigation (Chat, Monitoring, Config, Workflows)
  - Keyboard shortcuts and command routing
  - Streaming AI response handling
  - Configuration and workflow-specific key handlers

### 5. Enhanced View Rendering
- **Location**: `internal/cmd/dashboard_view.go`
- **Features**:
  - Responsive layout with panes
  - Animated loading indicators
  - Artifact display for generated configurations
  - Chat history rendering with roles and timestamps

## AI Streaming Issues Identified and Fixed

### Root Cause: Environment Variables Not Loaded
**Problem**: The `.env` file containing API keys was never loaded by the application.

**Evidence**:
```bash
# Direct curl test with OpenRouter - WORKED
curl -X POST "https://openrouter.ai/api/v1/chat/completions" \
  -H "Authorization: Bearer sk-or-v1-..." \
  -d '{"model": "anthropic/claude-3.5-sonnet", "messages": [...], "stream": true}'

# Response: Perfect streaming with content chunks
data: {"choices":[{"delta":{"content":"1\n2\n3"}}]}
```

**Solution**: Added `.env` loading to `runDashboard()` function:
```go
// Load .env file if it exists (for API keys)
if err := godotenv.Load(); err != nil {
    log.Printf("No .env file found or error loading it: %v", err)
}
```

### Streaming Implementation Fixes

#### 1. Enhanced Context System
- **File**: `dashboard_ai_config.go:enhancePromptWithConfigContext()`
- **Purpose**: Provides AI with rich context about current configurations, workflows, and templates
- **Context Includes**:
  - Current configuration resources and their validation status
  - Active workflows and their phases
  - Available configuration and workflow templates
  - Best practice guidelines

#### 2. Improved Stream Handling
- **File**: `dashboard_tui.go:handleChatStream()`
- **Improvements**:
  - Better logic to find streaming assistant messages
  - Proper content accumulation
  - Artifact detection and validation
  - Streaming completion handling

#### 3. Animated Loading Indicators
- **File**: `dashboard_view.go` + `dashboard.go:getAnimatedDots()`
- **Features**:
  - Animated dots (., .., ..., "") cycling every 300ms
  - UI refresh timer during streaming (200ms intervals)
  - Automatic cleanup when streaming completes

#### 4. Command Routing Fixes
- **File**: `dashboard_tui.go:sendMessage()`
- **Fixes**:
  - Proper separation of configuration commands vs system commands
  - Enhanced context for AI requests without showing system prompts to users
  - Streaming state management

## Ollama Troubleshooting

### Current Issue
**Symptoms**:
- Ollama shows GPU activity when messages are sent
- No responses appear in the TUI chat window
- No API keys required (local model)
- Streaming appears to start but content never displays

### Potential Causes

#### 1. Different Streaming Format
Ollama uses a different API format than OpenRouter/OpenAI. OpenRouter uses:
```json
data: {"choices":[{"delta":{"content":"text"}}]}
```

Ollama might use:
```json
{"model":"llama3","created_at":"...","message":{"content":"text"},"done":false}
```

**Investigation Needed**: Check `internal/ai/ollama.go` streaming implementation.

#### 2. Response Parsing Issues
- **File**: `internal/ai/ollama.go:StreamChat()`
- **Potential Issue**: The JSON parsing in the Ollama provider might not match the actual response format
- **Debug Approach**: Add logging to see raw responses from Ollama

#### 3. Content Accumulation
- **Potential Issue**: Ollama responses might not have the expected field structure
- **Check**: Verify the `StreamResponse.Content` field is being populated correctly

#### 4. Local Network/Endpoint Issues
- **Default Endpoint**: `http://localhost:11434`
- **Potential Issues**:
  - Ollama server not running
  - Different port or host
  - HTTP vs HTTPS issues
  - CORS or connection problems

### Debugging Steps for Ollama

#### 1. Test Ollama API Directly
```bash
# Test basic completion
curl http://localhost:11434/api/generate \
  -d '{"model": "llama3", "prompt": "Hello", "stream": false}'

# Test streaming
curl http://localhost:11434/api/generate \
  -d '{"model": "llama3", "prompt": "Count 1 to 5", "stream": true}'
```

#### 2. Check Ollama Provider Implementation
```go
// Add debug logging in internal/ai/ollama.go
fmt.Printf("DEBUG: Ollama raw response: %s\n", line)
```

#### 3. Verify Model Availability
```bash
# List available models
curl http://localhost:11434/api/tags

# Check if model is pulled
ollama list
```

#### 4. Environment Configuration
In `matey.yaml` or environment:
```yaml
ai:
  providers:
    ollama:
      endpoint: "http://localhost:11434"
      default_model: "llama3"  # or whatever model is installed
```

### Comparison: Working vs Non-Working Providers

#### ✅ OpenRouter (Working)
- **API Format**: OpenAI-compatible
- **Authentication**: API key in headers
- **Streaming**: SSE with `data:` prefixed JSON
- **Content Path**: `choices[0].delta.content`

#### ❌ Ollama (Not Working)
- **API Format**: Ollama-specific
- **Authentication**: None (local)
- **Streaming**: Newline-delimited JSON
- **Content Path**: `message.content` or similar

## Key Code Locations

### Core Files
- `internal/cmd/dashboard.go` - Main dashboard model and initialization
- `internal/cmd/dashboard_tui.go` - TUI event handling and streaming
- `internal/cmd/dashboard_view.go` - Rendering and layout
- `internal/cmd/dashboard_config.go` - Configuration management
- `internal/cmd/dashboard_workflows.go` - Workflow management
- `internal/cmd/dashboard_ai_config.go` - AI configuration assistant

### AI Integration
- `internal/ai/manager.go` - AI provider management
- `internal/ai/openrouter.go` - OpenRouter provider (working)
- `internal/ai/ollama.go` - Ollama provider (needs debugging)
- `internal/ai/openai.go` - OpenAI provider
- `internal/ai/claude.go` - Claude provider

### Test Files
- `internal/cmd/dashboard_ai_config_test.go` - AI configuration tests
- `internal/cmd/dashboard_tui_test.go` - TUI interaction tests
- `internal/cmd/dashboard_ai_integration_test.go` - AI integration tests

## Environment Setup

### Required Environment Variables
```bash
# .env file
OPENROUTER_API_KEY=sk-or-v1-...
ANTHROPIC_API_KEY=sk-ant-...
OPENAI_API_KEY=sk-...
OLLAMA_ENDPOINT=http://localhost:11434  # if different from default
```

### Dependencies Added
```go
go get github.com/joho/godotenv  // For .env loading
```

## Next Steps

### High Priority
1. **Fix Ollama Streaming**: Debug the Ollama provider streaming implementation
2. **Add Better Error Reporting**: Show specific provider errors in the UI
3. **Model Management**: Better model selection and availability checking

### Medium Priority
1. **Visual Workflow Designer**: Implement the pending workflow designer feature
2. **Configuration Persistence**: Save and load custom configurations
3. **Provider Health Monitoring**: Real-time provider status in the UI

### Low Priority
1. **Keyboard Shortcuts Help**: Better help system for keyboard navigation
2. **Custom Themes**: Allow customization of the TUI appearance
3. **Export/Import**: Configuration and workflow export/import functionality

## Lessons Learned

1. **Environment Loading**: Always verify environment variables are loaded in CLI applications
2. **API Testing**: Test APIs directly with curl before debugging application code
3. **Streaming Debugging**: Different providers have different streaming formats
4. **Animation Timing**: UI animations need proper refresh cycles to be visible
5. **Error Visibility**: Make errors visible in the UI for better debugging

## Testing Commands

### Working (OpenRouter)
```bash
./matey dashboard
# In chat:
/provider openrouter
testing a chat completion
/generate-config mcpserver test
```

### Needs Investigation (Ollama)
```bash
./matey dashboard
# In chat:
/provider ollama
/model llama3  # or whatever model is available
testing a chat completion
```

## Architecture Notes

The TUI uses the Bubble Tea framework with the following pattern:
- **Model**: Holds application state
- **Update**: Handles messages and state changes
- **View**: Renders the current state
- **Commands**: Async operations that generate messages

The AI integration follows a provider pattern with fallback support and streaming capabilities. Each provider implements the `Provider` interface with `StreamChat()` for real-time responses.