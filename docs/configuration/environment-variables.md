# Environment Variables Reference

This document describes all environment variables that can be used in Matey configuration files and their purposes.

## Variable Substitution

Matey supports environment variable substitution in configuration files using the `${VARIABLE_NAME}` syntax:

```yaml
services:
  web-search:
    environment:
      - SEARCH_API_KEY=${SEARCH_API_KEY}
      - DB_PASSWORD=${DB_PASSWORD}
```

## Core Environment Variables

### Database Configuration

```bash
# PostgreSQL connection
DB_HOST=localhost                    # Database host
DB_PORT=5432                        # Database port
DB_NAME=matey                       # Database name
DB_USER=matey                       # Database user
DB_PASSWORD=secure_password         # Database password
DB_SSL_MODE=require                 # SSL mode (disable, require, verify-ca, verify-full)

# Connection pooling
DB_MAX_CONNECTIONS=100              # Maximum connections
DB_MIN_CONNECTIONS=5                # Minimum connections
DB_CONNECTION_TIMEOUT=30s           # Connection timeout
DB_IDLE_TIMEOUT=10m                 # Idle connection timeout

# Replication (for HA setups)
DB_REPLICATION_USER=replicator      # Replication user
DB_REPLICATION_PASSWORD=repl_pass   # Replication password
DB_REPLICATION_MODE=master          # Replication mode
```

### AI Provider Configuration

#### OpenAI
```bash
OPENAI_API_KEY=sk-...               # OpenAI API key
OPENAI_ORG_ID=org-...               # Organization ID (optional)
OPENAI_BASE_URL=https://api.openai.com  # Custom base URL
OPENAI_MODEL=gpt-4                  # Default model
OPENAI_MAX_TOKENS=4000              # Default max tokens
OPENAI_TEMPERATURE=0.7              # Default temperature
OPENAI_TIMEOUT=30s                  # Request timeout
```

#### Claude (Anthropic)
```bash
CLAUDE_API_KEY=sk-ant-...           # Claude API key
CLAUDE_BASE_URL=https://api.anthropic.com  # Custom base URL
CLAUDE_MODEL=claude-3-sonnet-20240229  # Default model
CLAUDE_MAX_TOKENS=4000              # Default max tokens
CLAUDE_TEMPERATURE=0.7              # Default temperature
CLAUDE_TIMEOUT=30s                  # Request timeout
```

#### Ollama
```bash
OLLAMA_ENDPOINT=http://ollama:11434  # Ollama endpoint
OLLAMA_MODEL=llama2:7b              # Default model
OLLAMA_CONTEXT_LENGTH=4096          # Context window
OLLAMA_TEMPERATURE=0.7              # Default temperature
OLLAMA_TIMEOUT=60s                  # Request timeout
OLLAMA_KEEP_ALIVE=5m                # Keep model loaded
```

#### OpenRouter
```bash
OPENROUTER_API_KEY=sk-or-...        # OpenRouter API key
OPENROUTER_BASE_URL=https://openrouter.ai  # Base URL
OPENROUTER_MODEL=microsoft/wizardlm-2-8x22b  # Default model
OPENROUTER_SITE_URL=https://myapp.com  # Your site URL
OPENROUTER_APP_NAME=MyApp           # Your app name
```

### Authentication Configuration

#### JWT
```bash
JWT_SECRET=your-super-secret-key    # JWT signing secret
JWT_ALGORITHM=HS256                 # JWT algorithm
JWT_EXPIRATION=24h                  # Token expiration
JWT_REFRESH_EXPIRATION=7d           # Refresh token expiration
JWT_ISSUER=matey                    # JWT issuer
JWT_AUDIENCE=mcp-servers            # JWT audience
```

#### OAuth Providers
```bash
# GitHub OAuth
GITHUB_CLIENT_ID=your-github-client-id
GITHUB_CLIENT_SECRET=your-github-client-secret

# Google OAuth
GOOGLE_CLIENT_ID=your-google-client-id
GOOGLE_CLIENT_SECRET=your-google-client-secret

# Azure OAuth
AZURE_CLIENT_ID=your-azure-client-id
AZURE_CLIENT_SECRET=your-azure-client-secret
AZURE_TENANT_ID=your-azure-tenant-id

# Custom OAuth
OAUTH_PROVIDER_URL=https://auth.example.com
OAUTH_CLIENT_ID=your-client-id
OAUTH_CLIENT_SECRET=your-client-secret
```

#### API Keys
```bash
# Admin API key
ADMIN_API_KEY=admin-key-here

# Read-only API key
READONLY_API_KEY=readonly-key-here

# Service API keys
SERVICE_API_KEY=service-key-here
PROXY_API_KEY=proxy-key-here
```

### Networking Configuration

```bash
# External URLs
EXTERNAL_URL=https://matey.example.com  # External URL
API_URL=https://api.matey.example.com   # API URL
WEBHOOK_URL=https://hooks.matey.example.com  # Webhook URL

# TLS Configuration
TLS_CERT_PATH=/etc/ssl/certs/tls.crt   # TLS certificate path
TLS_KEY_PATH=/etc/ssl/private/tls.key  # TLS private key path
TLS_CA_PATH=/etc/ssl/certs/ca.crt      # CA certificate path

# Proxy Configuration
HTTP_PROXY=http://proxy.example.com:8080    # HTTP proxy
HTTPS_PROXY=https://proxy.example.com:8080  # HTTPS proxy
NO_PROXY=localhost,127.0.0.1,*.local       # No proxy hosts
```

### Service-Specific Configuration

#### Search Services
```bash
# Google Search
GOOGLE_API_KEY=your-google-api-key
GOOGLE_CSE_ID=your-custom-search-engine-id

# Bing Search
BING_API_KEY=your-bing-api-key

# Generic search
SEARCH_API_KEY=your-search-api-key
SEARCH_ENDPOINT=https://api.search.com
```

#### Git Services
```bash
# GitHub
GITHUB_TOKEN=ghp_your-github-token
GITHUB_USER=your-username
GITHUB_EMAIL=your-email@example.com

# GitLab
GITLAB_TOKEN=glpat-your-gitlab-token
GITLAB_USER=your-username
GITLAB_EMAIL=your-email@example.com

# Generic Git
GIT_USER=your-username
GIT_EMAIL=your-email@example.com
GIT_TOKEN=your-git-token
```

#### Docker Registry
```bash
# Docker Hub
DOCKER_USERNAME=your-docker-username
DOCKER_PASSWORD=your-docker-password

# Private Registry
REGISTRY_URL=registry.example.com
REGISTRY_USERNAME=your-registry-user
REGISTRY_PASSWORD=your-registry-password
```

### Monitoring Configuration

#### Prometheus
```bash
PROMETHEUS_ENDPOINT=http://prometheus:9090
PROMETHEUS_USER=prometheus
PROMETHEUS_PASSWORD=prometheus-password
PROMETHEUS_SCRAPE_INTERVAL=15s
```

#### Grafana
```bash
GRAFANA_ENDPOINT=http://grafana:3000
GRAFANA_ADMIN_USER=admin
GRAFANA_ADMIN_PASSWORD=admin-password
GRAFANA_ORG_ID=1
```

#### Logging
```bash
# Log levels: debug, info, warn, error
LOG_LEVEL=info

# Log formats: text, json
LOG_FORMAT=json

# Log outputs: stdout, file, syslog
LOG_OUTPUT=stdout

# Log file configuration
LOG_FILE=/var/log/matey.log
LOG_MAX_SIZE=100MB
LOG_MAX_FILES=10
LOG_COMPRESS=true
```

#### Tracing
```bash
# Jaeger
JAEGER_ENDPOINT=http://jaeger:14268/api/traces
JAEGER_SAMPLER_RATE=0.1
JAEGER_SERVICE_NAME=matey

# Zipkin
ZIPKIN_ENDPOINT=http://zipkin:9411/api/v2/spans
ZIPKIN_SAMPLER_RATE=0.1
```

#### Alerting
```bash
# Slack
SLACK_WEBHOOK_URL=https://hooks.slack.com/services/YOUR/SLACK/WEBHOOK
SLACK_CHANNEL=#alerts
SLACK_USERNAME=matey-bot

# Email
SMTP_HOST=smtp.example.com
SMTP_PORT=587
SMTP_USER=alerts@example.com
SMTP_PASSWORD=smtp-password
SMTP_FROM=matey-alerts@example.com
SMTP_TO=team@example.com

# PagerDuty
PAGERDUTY_INTEGRATION_KEY=your-pagerduty-key
PAGERDUTY_SEVERITY=error
```

### Kubernetes Configuration

```bash
# Namespace
KUBERNETES_NAMESPACE=default

# Service Account
KUBERNETES_SERVICE_ACCOUNT=matey-controller

# Cluster Configuration
KUBERNETES_CLUSTER_NAME=production
KUBERNETES_CONTEXT=production-context

# Resource Limits
DEFAULT_CPU_LIMIT=500m
DEFAULT_MEMORY_LIMIT=512Mi
DEFAULT_CPU_REQUEST=250m
DEFAULT_MEMORY_REQUEST=256Mi
```

### Feature Flags

```bash
# Core Features
ENABLE_AI_INTEGRATION=true
ENABLE_WORKFLOW_ENGINE=true
ENABLE_MEMORY_SERVICE=true
ENABLE_TASK_SCHEDULER=true
ENABLE_AUDIT_LOGGING=true
ENABLE_METRICS=true
ENABLE_TRACING=true
ENABLE_DASHBOARD=true

# Experimental Features
ENABLE_AUTO_SCALING=false
ENABLE_SERVICE_MESH=false
ENABLE_GITOPS=false
ENABLE_SMART_ROUTING=false
```

## Environment Files

### .env File Structure

Create a `.env` file in your project root:

```bash
# .env
# Core configuration
DB_PASSWORD=secure_password
JWT_SECRET=your-super-secret-key

# AI Providers
OPENAI_API_KEY=sk-your-openai-key
CLAUDE_API_KEY=sk-ant-your-claude-key
OLLAMA_ENDPOINT=http://ollama:11434

# OAuth
GITHUB_CLIENT_ID=your-github-client-id
GITHUB_CLIENT_SECRET=your-github-client-secret

# Monitoring
PROMETHEUS_ENDPOINT=http://prometheus:9090
GRAFANA_ADMIN_PASSWORD=admin-password
SLACK_WEBHOOK_URL=https://hooks.slack.com/services/YOUR/SLACK/WEBHOOK

# External services
SEARCH_API_KEY=your-search-api-key
GITHUB_TOKEN=ghp_your-github-token
```

### Environment-Specific Files

#### Development (.env.dev)
```bash
# Development environment
LOG_LEVEL=debug
LOG_FORMAT=text
ENABLE_DEBUG=true
ENABLE_HOT_RELOAD=true

# Use local services
DB_HOST=localhost
PROMETHEUS_ENDPOINT=http://localhost:9090
GRAFANA_ENDPOINT=http://localhost:3000

# Reduced resource limits
DEFAULT_CPU_LIMIT=200m
DEFAULT_MEMORY_LIMIT=256Mi
```

#### Staging (.env.staging)
```bash
# Staging environment
LOG_LEVEL=info
LOG_FORMAT=json
ENABLE_DEBUG=false

# Staging services
DB_HOST=staging-db.example.com
PROMETHEUS_ENDPOINT=http://staging-prometheus:9090
GRAFANA_ENDPOINT=http://staging-grafana:3000

# Staging resources
DEFAULT_CPU_LIMIT=500m
DEFAULT_MEMORY_LIMIT=512Mi
```

#### Production (.env.prod)
```bash
# Production environment
LOG_LEVEL=warn
LOG_FORMAT=json
ENABLE_DEBUG=false
ENABLE_AUDIT_LOGGING=true

# Production services
DB_HOST=prod-db.example.com
PROMETHEUS_ENDPOINT=http://prod-prometheus:9090
GRAFANA_ENDPOINT=http://prod-grafana:3000

# Production resources
DEFAULT_CPU_LIMIT=1000m
DEFAULT_MEMORY_LIMIT=1Gi
```

## Using Environment Files

### In Configuration
```yaml
# matey.yaml
version: "3.8"

services:
  web-search:
    environment:
      - SEARCH_API_KEY=${SEARCH_API_KEY}
      - DB_PASSWORD=${DB_PASSWORD}
      
ai_providers:
  - name: openai
    api_key: ${OPENAI_API_KEY}
    model: ${OPENAI_MODEL:-gpt-4}  # Default value
    
auth:
  jwt:
    secret: ${JWT_SECRET}
    expiration: ${JWT_EXPIRATION:-24h}
```

### Loading Environment Files
```bash
# Load specific environment file
export $(cat .env.dev | xargs)
matey up

# Load multiple files (later files override earlier ones)
export $(cat .env .env.local | xargs)
matey up

# Use with different configurations
export $(cat .env.prod | xargs)
matey -c matey-prod.yaml up
```

## Security Best Practices

### 1. Never Commit Secrets
```bash
# .gitignore
.env
.env.local
.env.*.local
secrets/
```

### 2. Use Strong Secrets
```bash
# Generate secure JWT secret
JWT_SECRET=$(openssl rand -hex 32)

# Generate secure database password
DB_PASSWORD=$(openssl rand -base64 32)

# Generate secure API key
API_KEY=$(openssl rand -hex 20)
```

### 3. Environment-Specific Secrets
```bash
# Use different secrets for different environments
JWT_SECRET_DEV=$(openssl rand -hex 32)
JWT_SECRET_STAGING=$(openssl rand -hex 32)
JWT_SECRET_PROD=$(openssl rand -hex 32)
```

### 4. Kubernetes Secrets
```bash
# Create Kubernetes secret from env file
kubectl create secret generic matey-secrets \
  --from-env-file=.env.prod

# Use in deployment
apiVersion: apps/v1
kind: Deployment
spec:
  template:
    spec:
      containers:
      - name: matey
        envFrom:
        - secretRef:
            name: matey-secrets
```

## Validation and Testing

### Environment Validation
```bash
# Check required variables
matey validate --check-env

# Test with specific environment
export $(cat .env.test | xargs)
matey validate

# Dry run with environment
export $(cat .env.prod | xargs)
matey up --dry-run
```

### Environment Templates
```bash
# Create environment template
cat > .env.template << 'EOF'
# Database
DB_PASSWORD=CHANGE_ME
DB_HOST=localhost

# AI Providers
OPENAI_API_KEY=CHANGE_ME
CLAUDE_API_KEY=CHANGE_ME

# OAuth
GITHUB_CLIENT_ID=CHANGE_ME
GITHUB_CLIENT_SECRET=CHANGE_ME

# Monitoring
SLACK_WEBHOOK_URL=CHANGE_ME
EOF
```

This comprehensive environment variables reference should help you configure Matey for any deployment scenario while maintaining security and flexibility.