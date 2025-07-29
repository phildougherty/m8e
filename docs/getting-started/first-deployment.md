# Your First Deployment

This guide walks you through deploying your first MCP server with Matey, explaining each step in detail.

## Understanding the Basics

### What is an MCP Server?

An MCP (Model Context Protocol) server is a service that provides:
- **Tools**: Functions that AI models can call
- **Resources**: Data that AI models can access
- **Prompts**: Templates for AI interactions

### How Matey Works

Matey takes your Docker Compose-style configuration and:
1. Translates it to Kubernetes resources
2. Manages the lifecycle with controllers
3. Provides service discovery and networking
4. Handles authentication and security

## Step-by-Step Deployment

### Step 1: Choose Your First MCP Server

Let's start with a simple filesystem server that allows AI models to read and write files:

```yaml
# matey.yaml
version: "3.8"

services:
  filesystem:
    image: mcp/filesystem-server:latest
    protocol: stdio
    command: ["npx", "-y", "@modelcontextprotocol/server-filesystem", "/data"]
    volumes:
      - ./data:/data
    resources:
      limits:
        memory: "128Mi"
        cpu: "100m"
      requests:
        memory: "64Mi"
        cpu: "50m"
```

### Step 2: Create the Data Directory

```bash
# Create local data directory
mkdir -p ./data
echo "Hello from MCP!" > ./data/welcome.txt
```

### Step 3: Deploy the Service

```bash
# Deploy the filesystem server
matey up filesystem

# Check deployment status
matey ps
```

Output should show:
```
NAME         STATUS    PROTOCOL  REPLICAS  READY  AGE
filesystem   Running   stdio     1/1       1      30s
```

### Step 4: Test the Server

```bash
# View server logs
matey logs filesystem

# Test the server directly
matey inspect filesystem --tools
```

### Step 5: Understanding What Happened

When you ran `matey up`, Matey created these Kubernetes resources:

```bash
# Check the created resources
kubectl get mcpservers
kubectl get deployments
kubectl get services
kubectl get configmaps
```

## Adding More Complexity

### Step 6: Add a Database-Backed Service

Let's add a more complex service that uses a database:

```yaml
# Add to matey.yaml
services:
  # ... existing filesystem service ...
  
  database:
    image: postgres:15
    environment:
      - POSTGRES_DB=mcpdata
      - POSTGRES_USER=mcpuser
      - POSTGRES_PASSWORD=mcppass
    volumes:
      - db-data:/var/lib/postgresql/data
    resources:
      limits:
        memory: "512Mi"
        cpu: "200m"
      requests:
        memory: "256Mi"
        cpu: "100m"
        
  database-tools:
    image: mcp/database-tools:latest
    protocol: http
    port: 8080
    environment:
      - DB_URL=postgresql://mcpuser:mcppass@database:5432/mcpdata
    depends_on:
      - database
    resources:
      limits:
        memory: "256Mi"
        cpu: "150m"
      requests:
        memory: "128Mi"
        cpu: "75m"

volumes:
  db-data:
```

### Step 7: Deploy the Updated Configuration

```bash
# Deploy all services
matey up

# Check status
matey ps
```

### Step 8: Test Service Communication

```bash
# Test database connection
matey exec database-tools -- psql -U mcpuser -d mcpdata -c "SELECT version();"

# Check tools available
matey inspect database-tools --tools
```

## Adding AI Integration

### Step 9: Configure AI Providers

```yaml
# Add to matey.yaml
ai_providers:
  - name: openai
    type: openai
    api_key: ${OPENAI_API_KEY}
    model: gpt-4
    max_tokens: 2000
    
  - name: claude
    type: claude
    api_key: ${CLAUDE_API_KEY}
    model: claude-3-sonnet-20240229
    max_tokens: 4000

# Enable AI features
features:
  ai_integration: true
  workflow_engine: true
  memory_service: true
```

### Step 10: Create Environment Variables

```bash
# .env file
OPENAI_API_KEY=sk-your-key-here
CLAUDE_API_KEY=sk-ant-your-key-here
```

### Step 11: Test AI Integration

```bash
# Reload configuration
matey reload

# Test AI chat
matey chat

# Try a filesystem operation via AI
# In the chat interface, try: "List the files in the /data directory"
```

## Monitoring and Debugging

### Step 12: Monitor Your Deployment

```bash
# Real-time status
matey top

# View all logs
matey logs --follow

# Get detailed service information
matey inspect filesystem --verbose
```

### Step 13: Debug Common Issues

```bash
# Check Kubernetes events
matey events

# Validate configuration
matey validate --verbose

# Test connectivity
matey ping filesystem
matey ping database-tools
```

## Understanding Resource Usage

### Step 14: Check Resource Consumption

```bash
# View resource usage
kubectl top pods

# Check resource limits
matey inspect filesystem --resources
```

### Step 15: Scale Your Services

```bash
# Scale filesystem server
matey scale filesystem=3

# Check scaling
matey ps
```

## Security Considerations

### Step 16: Enable Authentication

```yaml
# Add to matey.yaml
auth:
  enabled: true
  type: jwt
  secret: ${JWT_SECRET}
  
  # Optional: OAuth integration
  oauth:
    providers:
      - name: github
        client_id: ${GITHUB_CLIENT_ID}
        client_secret: ${GITHUB_CLIENT_SECRET}
```

### Step 17: Configure Network Policies

```yaml
# Add to matey.yaml
networking:
  policies:
    - name: filesystem-access
      from:
        - service: database-tools
      to:
        - service: filesystem
      ports:
        - protocol: TCP
          port: 8080
```

## Production Readiness

### Step 18: Add Health Checks

```yaml
# Update service configuration
services:
  filesystem:
    # ... existing config ...
    health:
      check:
        http:
          path: /health
          port: 8080
        interval: 30s
        timeout: 5s
        retries: 3
```

### Step 19: Configure Persistence

```yaml
# Add persistent volumes
volumes:
  filesystem-data:
    driver: kubernetes
    driver_opts:
      type: nfs
      server: nfs.example.com
      share: /exports/matey
      
  db-data:
    driver: kubernetes
    driver_opts:
      type: csi
      driver: ebs.csi.aws.com
      size: 20Gi
      type: gp3
```

### Step 20: Set Up Backup

```bash
# Create backup configuration
matey backup create --name daily-backup --schedule "0 2 * * *" --volumes filesystem-data,db-data
```

## Next Steps

Congratulations! You've successfully deployed your first MCP server with Matey. Here's what you can explore next:

### Advanced Features
- **[Service Discovery](../features/service-discovery.md)** - Automatic service discovery
- **[Workflow Engine](../features/workflow-engine.md)** - Complex workflow orchestration
- **[AI Integration](../features/ai-integration.md)** - Advanced AI provider configuration

### Configuration
- **[Configuration Reference](../configuration/matey-yaml.md)** - Complete configuration options
- **[Environment Variables](../configuration/environment-variables.md)** - Environment management
- **[Examples](../configuration/examples/)** - More example configurations

### Operations
- **[Deployment Strategies](../deployment/strategies.md)** - Production deployment patterns
- **[Monitoring](../deployment/monitoring.md)** - Observability and monitoring
- **[Troubleshooting](../troubleshooting.md)** - Common issues and solutions

### Development
- **[Contributing](../development/contributing.md)** - How to contribute to Matey
- **[Custom MCP Servers](../development/custom-servers.md)** - Building your own MCP servers

## Common Patterns

### Multi-Environment Setup

```yaml
# matey-dev.yaml
version: "3.8"
extends:
  file: matey.yaml
  service: base

services:
  filesystem:
    volumes:
      - ./data-dev:/data
      
# matey-prod.yaml
version: "3.8"
extends:
  file: matey.yaml
  service: base

services:
  filesystem:
    replicas: 3
    volumes:
      - prod-data:/data
```

### Service Mesh Integration

```yaml
# Add to matey.yaml
networking:
  service_mesh:
    enabled: true
    provider: istio
    features:
      - traffic_management
      - security
      - observability
```

### GitOps Workflow

```yaml
# .github/workflows/deploy.yml
name: Deploy to Kubernetes
on:
  push:
    branches: [main]

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v2
      - name: Deploy with Matey
        run: |
          matey validate
          matey up --wait
```

This completes your first deployment guide. You now have a solid foundation for building more complex MCP orchestrations with Matey!