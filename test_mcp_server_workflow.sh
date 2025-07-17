#!/bin/bash

# Test script for MCP server workflow functionality

echo "=== Testing Matey MCP Server Workflow Functionality ==="

# Start the MCP server in the background
echo "Starting MCP server..."
./matey mcp-server --port 8081 &
MCP_PID=$!

# Wait for server to start
sleep 3

# Test 1: Check if server is running
echo "=== Test 1: Health Check ==="
curl -s http://localhost:8081/health | jq '.' || echo "Health check failed"

# Test 2: List available tools
echo -e "\n=== Test 2: List Tools ==="
curl -s -X POST http://localhost:8081/ \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/list"
  }' | jq '.result.tools[] | select(.name == "create_workflow")' || echo "create_workflow tool not found"

# Test 3: Create a test workflow
echo -e "\n=== Test 3: Create Workflow ==="
WORKFLOW_RESPONSE=$(curl -s -X POST http://localhost:8081/ \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
      "name": "create_workflow",
      "arguments": {
        "name": "test-glucose-workflow",
        "description": "Test glucose tracking workflow",
        "schedule": "0 */6 * * *",
        "steps": [
          {
            "name": "check-glucose",
            "tool": "get_current_glucose",
            "parameters": {
              "unit": "mg/dL"
            }
          },
          {
            "name": "log-reading", 
            "tool": "log_glucose_reading",
            "parameters": {
              "source": "dexcom"
            },
            "dependsOn": ["check-glucose"]
          }
        ]
      }
    }
  }')

echo "Workflow creation response:"
echo "$WORKFLOW_RESPONSE" | jq '.'

# Test 4: Check if workflow was created (requires kubectl and the CRD to be installed)
echo -e "\n=== Test 4: Check Workflow Creation ==="
if command -v kubectl &> /dev/null; then
    # Check if the workflow CRD exists
    if kubectl get crd workflows.mcp.matey.ai &> /dev/null; then
        echo "Workflow CRD exists, checking for created workflow..."
        kubectl get workflows test-glucose-workflow -o yaml || echo "Workflow not found in cluster"
    else
        echo "Workflow CRD not installed. To install, run: kubectl apply -f config/crd/workflow.yaml"
    fi
else
    echo "kubectl not found, skipping cluster check"
fi

# Clean up
echo -e "\n=== Cleanup ==="
kill $MCP_PID 2>/dev/null || true
echo "MCP server stopped"

echo -e "\n=== Test Complete ==="