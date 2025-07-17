# AI Cluster Integration Setup Guide

This guide explains how to set up the new AI cluster integration that allows the Matey AI to actually execute commands and make changes to your live cluster.

## 🏗️ Architecture Overview

The integration consists of two main components:

### 1. **Enhanced TUI with MCP Client**
- The TUI AI can now connect to MCP servers running in the cluster
- AI can execute MCP tools and get real-time results
- Supports full conversation context with live cluster state

### 2. **Matey MCP Server**
- New MCP server that provides access to `matey` CLI commands
- Runs in the cluster with appropriate permissions
- Exposes tools for cluster management and configuration

## 🔧 Setup Instructions

### Step 1: Build the Updated Application

```bash
# Build the updated matey binary with MCP integration
make build

# Verify the new MCP server command is available
./bin/matey mcp-server --help
```

### Step 2: Deploy the Matey MCP Server

```bash
# Deploy the MCP server to your cluster
kubectl apply -f k8s/matey-mcp-server.yaml

# Check deployment status
kubectl get pods -l app=matey-mcp-server
kubectl logs -f deployment/matey-mcp-server
```

### Step 3: Configure MCP Proxy (if not already running)

```bash
# Start the MCP proxy to handle connections
matey proxy --port 8080

# The proxy should now include the matey MCP server
curl http://localhost:8080/mcp/servers
```

### Step 4: Test the Integration

```bash
# Start the enhanced TUI
./bin/matey tui

# The AI should now show available MCP tools in the welcome message
# Try asking: \"What's the current status of my cluster?\"
```

## 🚀 Available AI Capabilities

### Cluster Management
- **Check Status**: AI can run `matey ps` to see server status
- **Start/Stop Services**: AI can execute `matey up/down` commands
- **View Logs**: AI can fetch logs from specific services
- **Inspect Resources**: AI can get detailed resource information

### Configuration Management
- **Apply Configurations**: AI can apply YAML configurations to the cluster
- **Create Workflows**: AI can create and deploy workflow resources
- **Manage Toolboxes**: AI can set up MCP toolbox collections

### Real-time Monitoring
- **Live Status**: AI can watch server status in real-time
- **Log Streaming**: AI can follow logs from running services
- **Resource Monitoring**: AI can check resource usage and health

## 📋 Example AI Interactions

### 1. **Check Current Status**
```
User: What's the current status of my MCP servers?

AI: I'll check the current status of your MCP servers for you.

CALL matey.matey_ps {\"watch\": false}

✅ matey.matey_ps executed successfully:
[Shows current server status table]

Based on the current status, I can see that you have 3 servers running...
```

### 2. **Deploy a New Workflow**
```
User: Create a workflow that monitors my blood sugar every hour

AI: I'll create a blood sugar monitoring workflow for you.

CALL matey.create_workflow {
  \"name\": \"blood-sugar-monitor\",
  \"description\": \"Hourly blood sugar monitoring\",
  \"schedule\": \"0 * * * *\",
  \"steps\": [
    {
      \"name\": \"fetch-glucose\",
      \"tool\": \"dexcom/get_current_glucose\",
      \"parameters\": {}
    },
    {
      \"name\": \"store-data\",
      \"tool\": \"memory/store_glucose_reading\",
      \"parameters\": {
        \"glucose\": \"{{ .fetch-glucose.output.glucose }}\",
        \"timestamp\": \"{{ .fetch-glucose.output.timestamp }}\"
      }
    }
  ]
}

✅ matey.create_workflow executed successfully:
Workflow 'blood-sugar-monitor' created successfully...
```

### 3. **Apply Configuration Changes**
```
User: I want to scale up my memory server to 2 replicas

AI: I'll scale up your memory server to 2 replicas.

CALL matey.apply_config {
  \"config_yaml\": \"
apiVersion: mcp.matey.ai/v1
kind: MCPServer
metadata:
  name: memory
spec:
  replicas: 2
  ...
\"
}

✅ matey.apply_config executed successfully:
Configuration applied successfully. The memory server is now scaling to 2 replicas.
```

## 🔐 Security Considerations

### RBAC Permissions
The Matey MCP server runs with cluster-wide permissions to:
- Read/write MCP resources
- Manage pods and services
- Apply configurations
- Access logs and metrics

### Network Security
- MCP server only accessible within the cluster
- TUI connects through the MCP proxy
- All communications use standard HTTP/MCP protocols

### Authentication
- Service account based authentication
- No external API keys required for cluster operations
- Inherits cluster security policies

## 🛠️ Troubleshooting

### TUI Can't Connect to MCP Servers
```bash
# Check if MCP proxy is running
curl http://localhost:8080/health

# Check if Matey MCP server is deployed
kubectl get pods -l app=matey-mcp-server

# Check MCP server logs
kubectl logs -f deployment/matey-mcp-server
```

### Tool Calls Failing
```bash
# Check MCP server permissions
kubectl auth can-i create pods --as=system:serviceaccount:default:matey-mcp-server

# Check matey binary is available in the container
kubectl exec -it deployment/matey-mcp-server -- which matey
```

### Performance Issues
```bash
# Monitor resource usage
kubectl top pods -l app=matey-mcp-server

# Check for memory/CPU limits
kubectl describe pod -l app=matey-mcp-server
```

## 📈 Usage Examples

### Blood Sugar Monitoring Workflow
```yaml
# AI can create this automatically when you ask:
# \"Set up automated blood sugar monitoring with reports\"

apiVersion: mcp.matey.ai/v1
kind: Workflow
metadata:
  name: blood-sugar-automation
spec:
  schedule: \"0 */6 * * *\"  # Every 6 hours
  steps:
  - name: fetch-glucose-data
    tool: dexcom.get_glucose_readings
    parameters:
      hours: 6
  - name: analyze-trends
    tool: memory.analyze_glucose_trends
    parameters:
      data: \"{{ .fetch-glucose-data.output }}\"
  - name: generate-report
    tool: filesystem.write_file
    parameters:
      path: \"/home/phil/reports/glucose-{{ .timestamp }}.md\"
      content: \"{{ .analyze-trends.output.report }}\"
```

### Development Workflow
```yaml
# AI can create this when you ask:
# \"Set up a workflow that monitors my GitHub activity\"

apiVersion: mcp.matey.ai/v1
kind: Workflow
metadata:
  name: github-monitor
spec:
  schedule: \"0 9 * * 1-5\"  # Weekdays at 9 AM
  steps:
  - name: get-recent-commits
    tool: github.get_commits
    parameters:
      since: \"24h\"
  - name: analyze-activity
    tool: sequential-thinking.analyze_productivity
    parameters:
      commits: \"{{ .get-recent-commits.output }}\"
  - name: store-insights
    tool: memory.store_productivity_data
    parameters:
      analysis: \"{{ .analyze-activity.output }}\"
```

## 🔄 Next Steps

1. **Test the Integration**: Start with simple status checks
2. **Explore Capabilities**: Try different MCP tool combinations
3. **Create Workflows**: Set up automation for your specific needs
4. **Monitor Performance**: Keep an eye on resource usage
5. **Extend Functionality**: Add more MCP servers as needed

## 📚 Related Documentation

- [MCP Protocol Specification](https://modelcontextprotocol.io/docs)
- [Matey Configuration Guide](./README.md)
- [Kubernetes CRD Documentation](./config/crd/)
- [Workflow Examples](./examples/workflows/)

---

**Note**: This is a powerful integration that gives the AI direct access to your cluster. Always review AI-generated configurations before applying them to production environments.