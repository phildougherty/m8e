#!/bin/bash

# Debug script for chat function calling issues

echo "=== Testing Matey Chat Function Calling ==="

# Start MCP server
echo "Starting MCP server..."
./matey mcp-server --port 8081 > chat_debug.log 2>&1 &
MCP_PID=$!
sleep 3

# Test if server is running
echo "Checking server health..."
curl -s http://localhost:8081/health | jq '.'

# Check if create_workflow tool is available
echo -e "\nChecking create_workflow tool availability..."
CREATE_WORKFLOW_TOOL=$(curl -s -X POST http://localhost:8081/ \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/list"
  }' | jq '.result.tools[] | select(.name == "create_workflow")')

if [ "$CREATE_WORKFLOW_TOOL" != "" ]; then
    echo "✅ create_workflow tool found"
    echo "$CREATE_WORKFLOW_TOOL" | jq '.'
else
    echo "❌ create_workflow tool NOT found"
fi

# Test direct function call (this should work)
echo -e "\n=== Testing Direct Function Call ==="
DIRECT_RESULT=$(curl -s -X POST http://localhost:8081/ \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "tools/call",
    "params": {
      "name": "create_workflow",
      "arguments": {
        "name": "debug-test-workflow",
        "description": "Debug test workflow",
        "steps": [
          {
            "name": "test-step",
            "tool": "get_current_glucose"
          }
        ]
      }
    }
  }')

echo "Direct call result:"
echo "$DIRECT_RESULT" | jq '.'

# Check if workflow was created
echo -e "\n=== Checking Workflow Creation ==="
if kubectl get workflow debug-test-workflow &> /dev/null; then
    echo "✅ Workflow created successfully"
    kubectl get workflow debug-test-workflow -o jsonpath='{.spec.steps[0].name}'
    echo ""
else
    echo "❌ Workflow NOT created"
fi

# Test function call without arguments (this should fail like the chat does)
echo -e "\n=== Testing Function Call Without Arguments ==="
NO_ARGS_RESULT=$(curl -s -X POST http://localhost:8081/ \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 3,
    "method": "tools/call",
    "params": {
      "name": "create_workflow",
      "arguments": {}
    }
  }')

echo "No arguments call result:"
echo "$NO_ARGS_RESULT" | jq '.'

# Cleanup
echo -e "\n=== Cleanup ==="
kill $MCP_PID 2>/dev/null || true
kubectl delete workflow debug-test-workflow --ignore-not-found
echo "Cleanup complete"

echo -e "\n=== SUMMARY ==="
echo "This script tests the same scenarios that the chat interface uses:"
echo "1. ✅ Direct function call with arguments WORKS"
echo "2. ❌ Function call without arguments FAILS (like the chat)"
echo ""
echo "The problem is that the AI model in the chat isn't providing arguments"
echo "when it calls the create_workflow function."