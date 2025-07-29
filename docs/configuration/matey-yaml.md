# Matey Configuration Reference

This document provides a complete reference for the `matey.yaml` configuration file.

## Configuration Structure

```yaml
version: "3.8"                    # Configuration version (required)
services: {}                      # Service definitions (required)
volumes: {}                       # Volume definitions (optional)
networks: {}                      # Network definitions (optional)
ai_providers: []                  # AI provider configurations (optional)
auth: {}                          # Authentication configuration (optional)
discovery: {}                     # Service discovery configuration (optional)
features: {}                      # Feature flags (optional)
networking: {}                    # Networking configuration (optional)
resources: {}                     # Resource management (optional)
monitoring: {}                    # Monitoring configuration (optional)
```

## Services Configuration

### Basic Service Definition

```yaml
services:
  service-name:
    image: string                 # Container image (required)
    protocol: string              # MCP protocol: http, sse, websocket, stdio
    port: integer                 # Port number (for network protocols)
    command: [string]             # Override container command
    args: [string]                # Command arguments
    environment: [string]         # Environment variables
    volumes: [string]             # Volume mounts
    depends_on: [string]          # Service dependencies
    replicas: integer             # Number of replicas (default: 1)
    restart: string               # Restart policy: always, on-failure, never
    labels: {string: string}      # Service labels
    annotations: {string: string} # Service annotations
```

### Protocol Configuration

#### HTTP Protocol
```yaml
services:
  http-server:
    image: mcp/http-server:latest
    protocol: http
    port: 8080
    health:
      check:
        http:
          path: /health
          port: 8080
        interval: 30s
        timeout: 5s
        retries: 3
```

#### Server-Sent Events (SSE)
```yaml
services:
  sse-server:
    image: mcp/sse-server:latest
    protocol: sse
    port: 8080
    sse:
      endpoint: /events
      heartbeat_interval: 30s
      max_connections: 1000
```

#### WebSocket
```yaml
services:
  websocket-server:
    image: mcp/websocket-server:latest
    protocol: websocket
    port: 8080
    websocket:
      endpoint: /ws
      max_message_size: 1MB
      ping_interval: 30s
```

#### STDIO
```yaml
services:
  stdio-server:
    image: mcp/stdio-server:latest
    protocol: stdio
    command: ["node", "server.js"]
    stdin: true
    tty: true
```

### Resource Management

```yaml
services:
  resource-managed:
    image: mcp/server:latest
    resources:
      limits:
        memory: "512Mi"
        cpu: "500m"
        ephemeral-storage: "1Gi"
      requests:
        memory: "256Mi"
        cpu: "250m"
        ephemeral-storage: "512Mi"
    # GPU resources
    gpu:
      nvidia.com/gpu: 1
      limits:
        nvidia.com/gpu: 1
```

### Volume Configuration

```yaml
services:
  with-volumes:
    image: mcp/server:latest
    volumes:
      - "./data:/app/data"                    # Host path
      - "shared-data:/app/shared"             # Named volume
      - "config:/app/config:ro"               # Read-only
      - "/tmp:/app/tmp:rw,noexec"            # Mount options
```

### Environment Variables

```yaml
services:
  with-env:
    image: mcp/server:latest
    environment:
      - NODE_ENV=production
      - API_KEY=${API_KEY}                    # From .env file
      - DATABASE_URL=postgresql://user:pass@db:5432/mydb
      - DEBUG=true
    env_file:
      - .env
      - .env.local
```

### Security Configuration

```yaml
services:
  secure-service:
    image: mcp/server:latest
    security:
      run_as_user: 1000
      run_as_group: 1000
      fs_group: 2000
      read_only_root_filesystem: true
      allow_privilege_escalation: false
      capabilities:
        drop:
          - ALL
        add:
          - NET_BIND_SERVICE
      seccomp_profile:
        type: RuntimeDefault
      selinux_options:
        level: "s0:c123,c456"
```

## Volume Configuration

### Named Volumes

```yaml
volumes:
  database-data:
    driver: kubernetes
    driver_opts:
      type: hostPath
      path: /var/lib/database
      
  shared-storage:
    driver: kubernetes
    driver_opts:
      type: nfs
      server: nfs.example.com
      path: /exports/shared
      
  cloud-storage:
    driver: kubernetes
    driver_opts:
      type: csi
      driver: ebs.csi.aws.com
      size: 20Gi
      type: gp3
      encrypted: true
```

### Volume Types

#### Local Storage
```yaml
volumes:
  local-data:
    driver: kubernetes
    driver_opts:
      type: hostPath
      path: /opt/data
      create: true
```

#### Network Storage
```yaml
volumes:
  nfs-storage:
    driver: kubernetes
    driver_opts:
      type: nfs
      server: 192.168.1.100
      path: /exports/data
      options: "vers=4.0,rsize=1048576,wsize=1048576"
```

#### Cloud Storage
```yaml
volumes:
  aws-ebs:
    driver: kubernetes
    driver_opts:
      type: csi
      driver: ebs.csi.aws.com
      size: 50Gi
      type: gp3
      iops: 3000
      throughput: 125
      encrypted: true
      
  gcp-disk:
    driver: kubernetes
    driver_opts:
      type: csi
      driver: pd.csi.storage.gke.io
      size: 50Gi
      type: pd-ssd
      replication-type: regional-pd
```

## AI Provider Configuration

### OpenAI
```yaml
ai_providers:
  - name: openai
    type: openai
    api_key: ${OPENAI_API_KEY}
    model: gpt-4
    max_tokens: 2000
    temperature: 0.7
    top_p: 1.0
    frequency_penalty: 0.0
    presence_penalty: 0.0
    timeout: 30s
    retry_attempts: 3
    fallback: claude
```

### Claude
```yaml
ai_providers:
  - name: claude
    type: claude
    api_key: ${CLAUDE_API_KEY}
    model: claude-3-sonnet-20240229
    max_tokens: 4000
    temperature: 0.7
    top_p: 1.0
    timeout: 30s
    retry_attempts: 3
    system_prompt: "You are a helpful assistant."
```

### Ollama
```yaml
ai_providers:
  - name: ollama
    type: ollama
    endpoint: http://ollama:11434
    model: llama2:7b
    context_length: 4096
    temperature: 0.7
    top_p: 0.9
    timeout: 60s
    keep_alive: 5m
```

### OpenRouter
```yaml
ai_providers:
  - name: openrouter
    type: openrouter
    api_key: ${OPENROUTER_API_KEY}
    model: microsoft/wizardlm-2-8x22b
    max_tokens: 8000
    temperature: 0.7
    top_p: 1.0
    timeout: 45s
    site_url: https://myapp.com
    app_name: MyApp
```

## Authentication Configuration

### JWT Authentication
```yaml
auth:
  enabled: true
  type: jwt
  secret: ${JWT_SECRET}
  algorithm: HS256
  expiration: 24h
  refresh_enabled: true
  refresh_expiration: 7d
  issuer: matey
  audience: mcp-servers
```

### OAuth 2.1 Configuration
```yaml
auth:
  enabled: true
  type: oauth
  oauth:
    providers:
      - name: github
        client_id: ${GITHUB_CLIENT_ID}
        client_secret: ${GITHUB_CLIENT_SECRET}
        scopes: [user:email, read:user]
        redirect_uri: http://localhost:8080/auth/callback
        
      - name: google
        client_id: ${GOOGLE_CLIENT_ID}
        client_secret: ${GOOGLE_CLIENT_SECRET}
        scopes: [openid, email, profile]
        redirect_uri: http://localhost:8080/auth/google/callback
```

### API Key Authentication
```yaml
auth:
  enabled: true
  type: api_key
  api_keys:
    - name: admin
      key: ${ADMIN_API_KEY}
      scopes: [admin, read, write]
      
    - name: readonly
      key: ${READONLY_API_KEY}
      scopes: [read]
```

### RBAC Configuration
```yaml
auth:
  rbac:
    enabled: true
    roles:
      - name: admin
        permissions:
          - "servers:*"
          - "workflows:*"
          - "tools:*"
          
      - name: developer
        permissions:
          - "servers:read"
          - "servers:write"
          - "tools:execute"
          
      - name: readonly
        permissions:
          - "servers:read"
          - "tools:read"
```

## Service Discovery Configuration

```yaml
discovery:
  enabled: true
  provider: kubernetes
  cross_namespace: false
  health_check_interval: 30s
  timeout: 5s
  retries: 3
  
  # Custom service discovery
  custom:
    endpoints:
      - name: external-service
        url: http://external.example.com:8080
        health_check: http://external.example.com:8080/health
        
  # DNS configuration
  dns:
    search_domains:
      - svc.cluster.local
      - cluster.local
    nameservers:
      - 10.96.0.10
      - 8.8.8.8
```

## Feature Flags

```yaml
features:
  ai_integration: true
  workflow_engine: true
  memory_service: true
  task_scheduler: true
  audit_logging: true
  metrics: true
  tracing: true
  dashboard: true
  
  # Experimental features
  experimental:
    auto_scaling: true
    service_mesh: false
    gitops: false
```

## Networking Configuration

```yaml
networking:
  # Service mesh integration
  service_mesh:
    enabled: true
    provider: istio
    features:
      - traffic_management
      - security
      - observability
      
  # Network policies
  policies:
    - name: default-deny
      type: ingress
      action: deny
      
    - name: allow-frontend
      type: ingress
      action: allow
      from:
        - service: frontend
      to:
        - service: backend
      ports:
        - protocol: TCP
          port: 8080
          
  # Load balancing
  load_balancer:
    algorithm: round_robin
    health_check:
      path: /health
      interval: 10s
      timeout: 5s
      
  # TLS configuration
  tls:
    enabled: true
    cert_manager: true
    certificates:
      - name: api-cert
        domains:
          - api.example.com
        issuer: letsencrypt-prod
```

## Resource Management

```yaml
resources:
  # Default resource limits
  defaults:
    limits:
      memory: "512Mi"
      cpu: "500m"
    requests:
      memory: "256Mi"
      cpu: "250m"
      
  # Resource quotas
  quotas:
    namespace: default
    limits:
      requests.cpu: "4"
      requests.memory: "8Gi"
      limits.cpu: "8"
      limits.memory: "16Gi"
      persistentvolumeclaims: "10"
      
  # Horizontal Pod Autoscaler
  hpa:
    enabled: true
    min_replicas: 2
    max_replicas: 10
    target_cpu_utilization: 70
    target_memory_utilization: 80
    
  # Vertical Pod Autoscaler
  vpa:
    enabled: true
    update_mode: Auto
    min_allowed:
      cpu: 100m
      memory: 50Mi
    max_allowed:
      cpu: 1
      memory: 1Gi
```

## Monitoring Configuration

```yaml
monitoring:
  # Prometheus metrics
  prometheus:
    enabled: true
    endpoint: /metrics
    port: 9090
    scrape_interval: 15s
    
  # Grafana dashboards
  grafana:
    enabled: true
    dashboards:
      - name: matey-overview
        file: dashboards/overview.json
        
  # Alerting
  alerts:
    enabled: true
    webhook: https://hooks.slack.com/services/YOUR/SLACK/WEBHOOK
    rules:
      - name: high-cpu
        condition: cpu > 80
        duration: 5m
        severity: warning
        
      - name: service-down
        condition: up == 0
        duration: 1m
        severity: critical
        
  # Logging
  logging:
    level: info
    format: json
    outputs:
      - stdout
      - file: /var/log/matey.log
      
  # Tracing
  tracing:
    enabled: true
    jaeger:
      endpoint: http://jaeger:14268/api/traces
      sampler_rate: 0.1
```

## Environment Variables Reference

Common environment variables that can be used in configuration:

```bash
# Database
DB_HOST=localhost
DB_PORT=5432
DB_NAME=matey
DB_USER=matey
DB_PASSWORD=secret

# AI Providers
OPENAI_API_KEY=sk-...
CLAUDE_API_KEY=sk-ant-...
OLLAMA_ENDPOINT=http://ollama:11434

# Authentication
JWT_SECRET=your-secret-key
GITHUB_CLIENT_ID=your-github-client-id
GITHUB_CLIENT_SECRET=your-github-client-secret

# Networking
EXTERNAL_URL=https://matey.example.com
TLS_CERT_PATH=/etc/ssl/certs/tls.crt
TLS_KEY_PATH=/etc/ssl/private/tls.key

# Monitoring
PROMETHEUS_ENDPOINT=http://prometheus:9090
GRAFANA_ENDPOINT=http://grafana:3000
```

## Configuration Validation

```bash
# Validate configuration
matey validate

# Validate with verbose output
matey validate --verbose

# Validate specific section
matey validate --section services

# Check syntax only
matey validate --syntax-only

# Validate against schema
matey validate --schema
```

## Configuration Examples

See the [examples directory](examples/) for complete configuration examples:

- [basic.yaml](examples/basic.yaml) - Basic setup
- [production.yaml](examples/production.yaml) - Production configuration
- [multi-env.yaml](examples/multi-env.yaml) - Multi-environment setup
- [enterprise.yaml](examples/enterprise.yaml) - Enterprise features
- [ai-workflow.yaml](examples/ai-workflow.yaml) - AI-powered workflows

## Best Practices

1. **Use Environment Variables**: Keep secrets in environment variables
2. **Resource Limits**: Always set resource limits for production
3. **Health Checks**: Configure health checks for all services
4. **Security**: Enable authentication and network policies
5. **Monitoring**: Set up monitoring and alerting
6. **Backup**: Configure backup for persistent data
7. **Testing**: Validate configuration before deployment