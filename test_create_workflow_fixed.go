package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

func main() {
	// Start MCP server in the background
	go func() {
		// Give the server time to start
		time.Sleep(2 * time.Second)
		
		// Test create_workflow with direct HTTP request
		testCreateWorkflow()
	}()
	
	// Start the MCP server
	fmt.Println("Starting MCP server...")
	// This would normally be done by running: ./matey mcp-server --port 8081
	// For testing purposes, let's make direct HTTP requests
	
	// Wait for the goroutine to complete
	time.Sleep(5 * time.Second)
}

func testCreateWorkflow() {
	// Test 1: Test tools/list endpoint
	fmt.Println("=== Testing tools/list endpoint ===")
	toolsResp := makeJSONRPCRequest("tools/list", nil)
	fmt.Printf("Tools response: %s\n\n", toolsResp)
	
	// Test 2: Test create_workflow tool
	fmt.Println("=== Testing create_workflow tool ===")
	
	// Prepare workflow arguments
	arguments := map[string]interface{}{
		"name": "test-glucose-workflow",
		"description": "Test glucose tracking workflow",
		"schedule": "0 */6 * * *", // Every 6 hours
		"steps": []map[string]interface{}{
			{
				"name": "check-glucose",
				"tool": "get_current_glucose",
				"parameters": map[string]interface{}{
					"unit": "mg/dL",
				},
			},
			{
				"name": "log-reading",
				"tool": "log_glucose_reading",
				"parameters": map[string]interface{}{
					"source": "dexcom",
				},
				"dependsOn": []string{"check-glucose"},
			},
		},
	}
	
	params := map[string]interface{}{
		"name":      "create_workflow",
		"arguments": arguments,
	}
	
	result := makeJSONRPCRequest("tools/call", params)
	fmt.Printf("Create workflow result: %s\n", result)
}

func makeJSONRPCRequest(method string, params interface{}) string {
	request := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	}
	
	jsonData, err := json.Marshal(request)
	if err != nil {
		return fmt.Sprintf("Error marshaling request: %v", err)
	}
	
	resp, err := http.Post("http://localhost:8081/", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Sprintf("Error making request: %v", err)
	}
	defer resp.Body.Close()
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Sprintf("Error reading response: %v", err)
	}
	
	return string(body)
}