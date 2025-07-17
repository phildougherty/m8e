# Create Workflow Function Calling Issue - Analysis & Solution

## Issue Summary

The `create_workflow` tool works perfectly when called directly via curl/HTTP but fails when called through the matey chat TUI interface because the AI model isn't providing required arguments.

## Confirmed Working ✅

1. **MCP Server**: ✅ Works perfectly
2. **create_workflow Tool**: ✅ Works with proper arguments
3. **Tool Discovery**: ✅ Function is found and listed correctly
4. **Direct HTTP Calls**: ✅ Work perfectly

```bash
# This works perfectly:
curl -X POST http://localhost:8081/ -H "Content-Type: application/json" -d '{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "create_workflow",
    "arguments": {
      "name": "test-workflow",
      "steps": [{"name": "task1", "tool": "get_current_glucose"}]
    }
  }
}'
```

## Failing in Chat TUI ❌

```
DEBUG: Raw arguments for 'create_workflow': ''
DEBUG: No arguments provided, using empty map
❌ Error calling matey.create_workflow: MCP error: name is required
```

## Root Cause

The AI model (Claude Sonnet 4 via OpenRouter) is calling `create_workflow()` with no arguments when it should be calling:

```javascript
create_workflow({
  "name": "glucose-tracker", 
  "steps": [{"name": "check-glucose", "tool": "get_current_glucose"}]
})
```

## Solutions

### Solution 1: Fix Function Schema Conversion
The issue might be in how the MCP tool schema is converted to AI function schema in `getMCPFunctions()`.

### Solution 2: Add Argument Validation in Chat
Add better error handling and retry logic when the AI calls functions without required arguments.

### Solution 3: Improve Continuation Prompts
The continuation prompt is trying to provide instructions but isn't effective enough.

### Solution 4: Switch AI Provider for Testing
Test with different AI providers (ollama, claude direct) to see if it's an OpenRouter-specific issue.

## Immediate Workaround

Use curl directly to create workflows until the chat interface function calling is fixed:

```bash
curl -X POST http://localhost:8081/ -H "Content-Type: application/json" -d '{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "create_workflow",
    "arguments": {
      "name": "glucose-monthly-report",
      "description": "Monthly glucose monitoring with reports",
      "schedule": "0 9 1 * *",
      "steps": [
        {"name": "get-data", "tool": "get_current_glucose"},
        {"name": "generate-report", "tool": "generate_report", "parameters": {"type": "monthly"}}
      ]
    }
  }
}'
```

## Next Steps

1. Investigate the AI function calling implementation in `internal/ai/`
2. Check if the function schema is being passed correctly to OpenRouter
3. Test with different AI providers to isolate the issue
4. Add better error handling and retry logic for function calls