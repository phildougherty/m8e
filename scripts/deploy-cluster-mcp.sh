#!/bin/bash

# Deploy MCP Cluster Integration
# This script deploys the MCP proxy and Matey MCP server to the cluster

set -e

NAMESPACE="${NAMESPACE:-default}"
INGRESS_HOST="${INGRESS_HOST:-mcp.robotrad.io}"
API_KEY="${MCP_API_KEY:-$(openssl rand -hex 32)}"

echo "Deploying MCP Cluster Integration"
echo "Namespace: $NAMESPACE"
echo "Ingress Host: $INGRESS_HOST"
echo "API Key: ${API_KEY:0:8}..."
echo ""

# Create namespace if it doesn't exist
if ! kubectl get namespace "$NAMESPACE" >/dev/null 2>&1; then
    echo "Creating namespace: $NAMESPACE"
    kubectl create namespace "$NAMESPACE"
fi

# Update the ingress host in the deployment
sed -i "s/mcp.robotrad.io/$INGRESS_HOST/g" k8s/mcp-proxy-deployment.yaml

# Update the API key in the secret
sed -i "s/your-api-key-here/$API_KEY/g" k8s/mcp-proxy-deployment.yaml

# Deploy the MCP proxy
echo "Deploying MCP Proxy..."
kubectl apply -f k8s/mcp-proxy-deployment.yaml -n "$NAMESPACE"

# Deploy the Matey MCP server
echo "Deploying Matey MCP Server..."
kubectl apply -f k8s/matey-mcp-server-deployment.yaml -n "$NAMESPACE"

# Wait for deployments to be ready
echo "Waiting for deployments to be ready..."
kubectl rollout status deployment/matey-mcp-proxy -n "$NAMESPACE" --timeout=300s
kubectl rollout status deployment/matey-mcp-server -n "$NAMESPACE" --timeout=300s

# Get service information
echo ""
echo "Deployment completed successfully!"
echo ""
echo "Service Information:"
echo "MCP Proxy Service:"
kubectl get service matey-mcp-proxy -n "$NAMESPACE" -o wide

echo ""
echo "Matey MCP Server Service:"
kubectl get service matey-mcp-server -n "$NAMESPACE" -o wide

echo ""
echo "Access Information:"
echo "Ingress URL: http://$INGRESS_HOST"
echo "NodePort URL: http://localhost:30876"
echo "Port Forward: kubectl port-forward service/matey-mcp-proxy 9876:9876 -n $NAMESPACE"

echo ""
echo "Authentication:"
echo "Set MCP_API_KEY environment variable to: $API_KEY"
echo "export MCP_API_KEY=\"$API_KEY\""

echo ""
echo "Testing Connection:"
echo "curl -H \"Authorization: Bearer $API_KEY\" http://$INGRESS_HOST/health"
echo "curl -H \"Authorization: Bearer $API_KEY\" http://localhost:30876/health"

echo ""
echo "To use with matey TUI:"
echo "export MCP_API_KEY=\"$API_KEY\""
echo "matey chat"