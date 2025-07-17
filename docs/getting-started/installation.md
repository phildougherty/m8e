# Installation Guide

This guide covers all installation methods for Matey (m8e).

## Prerequisites

- **Kubernetes cluster** (1.28 or later)
- **kubectl** configured and connected to your cluster
- **Go 1.24+** (for building from source)
- **Helm 3.0+** (for Helm installation)

## Installation Methods

### Method 1: Binary Installation (Recommended)

#### Linux/macOS

```bash
# Download the latest release
curl -L https://github.com/phildougherty/m8e/releases/latest/download/matey-linux-amd64.tar.gz | tar xz
sudo mv matey /usr/local/bin/

# Verify installation
matey --version
```

#### Windows

```powershell
# Download from releases page
Invoke-WebRequest -Uri "https://github.com/phildougherty/m8e/releases/latest/download/matey-windows-amd64.zip" -OutFile "matey.zip"
Expand-Archive matey.zip
Move-Item matey\matey.exe C:\Windows\System32\
```

### Method 2: Build from Source

```bash
# Clone repository
git clone https://github.com/phildougherty/m8e.git
cd m8e

# Build
make build

# Install system-wide
sudo make install

# Or install for current user
make install-user
```

### Method 3: Helm Installation

```bash
# Add Helm repository
helm repo add matey https://phildougherty.github.io/m8e
helm repo update

# Install with default values
helm install matey matey/matey

# Install with custom values
helm install matey matey/matey -f values.yaml
```

### Method 4: Kubernetes Manifests

```bash
# Apply CRDs
kubectl apply -f https://raw.githubusercontent.com/phildougherty/m8e/main/config/crd/

# Apply controllers
kubectl apply -f https://raw.githubusercontent.com/phildougherty/m8e/main/k8s/
```

## Post-Installation Setup

### 1. Install CRDs and Controllers

```bash
# Install Matey CRDs and controllers in your cluster
matey install

# Verify installation
kubectl get crd | grep matey
kubectl get pods -n matey-system
```

### 2. Create Configuration

```bash
# Create a basic configuration
matey create-config --template quickstart

# Or create from existing examples
matey create-config --from-example basic
```

### 3. Validate Setup

```bash
# Validate your configuration
matey validate

# Test connectivity
matey ps
```

## Configuration

### Environment Variables

Create a `.env` file in your project directory:

```bash
# Database
DB_PASSWORD=secure_password

# AI Providers
OPENAI_API_KEY=your_openai_key
CLAUDE_API_KEY=your_claude_key

# OAuth
GITHUB_CLIENT_ID=your_github_client_id
GITHUB_CLIENT_SECRET=your_github_client_secret

# Search
SEARCH_API_KEY=your_search_key
```

### Shell Completion

```bash
# Bash
echo 'source <(matey completion bash)' >>~/.bashrc

# Zsh
echo 'source <(matey completion zsh)' >>~/.zshrc

# Fish
matey completion fish | source
```

## Troubleshooting

### Common Issues

#### 1. CRD Installation Fails

```bash
# Check cluster permissions
kubectl auth can-i create customresourcedefinitions

# Manual CRD installation
kubectl apply -f config/crd/
```

#### 2. Controller Not Starting

```bash
# Check controller logs
kubectl logs -n matey-system deployment/matey-controller

# Check RBAC permissions
kubectl describe clusterrole matey-controller
```

#### 3. Configuration Validation Errors

```bash
# Detailed validation
matey validate --verbose

# Check configuration syntax
matey validate --syntax-only
```

### Getting Help

- **Documentation**: Check the [troubleshooting guide](../troubleshooting.md)
- **Issues**: Report problems on [GitHub Issues](https://github.com/phildougherty/m8e/issues)
- **Community**: Join discussions on [GitHub Discussions](https://github.com/phildougherty/m8e/discussions)

## Next Steps

- [Quick Start Guide](quick-start.md) - Deploy your first MCP server
- [Configuration Reference](../configuration/matey-yaml.md) - Learn about configuration options
- [CLI Commands](../cli/commands-overview.md) - Explore available commands