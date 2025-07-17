# Helm Deployment Guide

This guide covers deploying Matey using Helm charts for various environments and use cases.

## Prerequisites

- Kubernetes cluster (1.28+)
- Helm 3.0+ installed
- `kubectl` configured

## Quick Start

### Add Matey Helm Repository

```bash
helm repo add matey https://phildougherty.github.io/m8e
helm repo update
```

### Install Matey

```bash
# Create namespace
kubectl create namespace matey-system

# Install with default values
helm install matey matey/matey -n matey-system

# Check installation
helm status matey -n matey-system
```

## Chart Configuration

### Default Values

```yaml
# Default values for matey chart
replicaCount: 1

image:
  repository: phildougherty/matey
  tag: "0.0.4"
  pullPolicy: IfNotPresent

nameOverride: ""
fullnameOverride: ""

serviceAccount:
  create: true
  annotations: {}
  name: ""

rbac:
  create: true

service:
  type: ClusterIP
  port: 8080

ingress:
  enabled: false
  className: ""
  annotations: {}
  hosts:
    - host: chart-example.local
      paths:
        - path: /
          pathType: Prefix
  tls: []

resources:
  limits:
    cpu: 500m
    memory: 512Mi
  requests:
    cpu: 250m
    memory: 256Mi

autoscaling:
  enabled: false
  minReplicas: 1
  maxReplicas: 100
  targetCPUUtilizationPercentage: 80

nodeSelector: {}
tolerations: []
affinity: {}

postgresql:
  enabled: true
  auth:
    username: matey
    password: ""
    database: matey
```

### Custom Values Configuration

Create a `values.yaml` file for your environment:

```yaml
# values.yaml
replicaCount: 3

image:
  repository: phildougherty/matey
  tag: "0.0.4"
  pullPolicy: Always

service:
  type: LoadBalancer
  port: 80

ingress:
  enabled: true
  className: "nginx"
  annotations:
    kubernetes.io/ingress.class: nginx
    cert-manager.io/cluster-issuer: letsencrypt-prod
    nginx.ingress.kubernetes.io/force-ssl-redirect: "true"
  hosts:
    - host: matey.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: matey-tls
      hosts:
        - matey.example.com

resources:
  limits:
    cpu: 1000m
    memory: 1Gi
  requests:
    cpu: 500m
    memory: 512Mi

autoscaling:
  enabled: true
  minReplicas: 3
  maxReplicas: 10
  targetCPUUtilizationPercentage: 70

postgresql:
  enabled: true
  auth:
    username: matey
    password: secure-password
    database: matey
  primary:
    persistence:
      enabled: true
      size: 20Gi
      storageClass: "fast-ssd"
```

## Environment-Specific Deployments

### Development Environment

```yaml
# values-dev.yaml
replicaCount: 1

image:
  tag: "latest"
  pullPolicy: Always

service:
  type: NodePort
  port: 8080

ingress:
  enabled: true
  className: "nginx"
  hosts:
    - host: matey-dev.local
      paths:
        - path: /
          pathType: Prefix

resources:
  limits:
    cpu: 200m
    memory: 256Mi
  requests:
    cpu: 100m
    memory: 128Mi

postgresql:
  enabled: true
  auth:
    username: matey
    password: dev-password
    database: matey_dev
  primary:
    persistence:
      enabled: false

# Development features
config:
  debug: true
  log_level: debug
  hot_reload: true
```

### Staging Environment

```yaml
# values-staging.yaml
replicaCount: 2

image:
  tag: "0.0.4"
  pullPolicy: IfNotPresent

service:
  type: ClusterIP
  port: 8080

ingress:
  enabled: true
  className: "nginx"
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-staging
  hosts:
    - host: matey-staging.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: matey-staging-tls
      hosts:
        - matey-staging.example.com

resources:
  limits:
    cpu: 500m
    memory: 512Mi
  requests:
    cpu: 250m
    memory: 256Mi

postgresql:
  enabled: true
  auth:
    username: matey
    password: staging-password
    database: matey_staging
  primary:
    persistence:
      enabled: true
      size: 10Gi
      storageClass: "standard"

# Staging features
config:
  debug: false
  log_level: info
  metrics_enabled: true
```

### Production Environment

```yaml
# values-prod.yaml
replicaCount: 5

image:
  tag: "0.0.4"
  pullPolicy: IfNotPresent

service:
  type: ClusterIP
  port: 8080

ingress:
  enabled: true
  className: "nginx"
  annotations:
    kubernetes.io/ingress.class: nginx
    cert-manager.io/cluster-issuer: letsencrypt-prod
    nginx.ingress.kubernetes.io/force-ssl-redirect: "true"
    nginx.ingress.kubernetes.io/rate-limit: "100"
  hosts:
    - host: matey.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: matey-prod-tls
      hosts:
        - matey.example.com

resources:
  limits:
    cpu: 1000m
    memory: 1Gi
  requests:
    cpu: 500m
    memory: 512Mi

autoscaling:
  enabled: true
  minReplicas: 5
  maxReplicas: 20
  targetCPUUtilizationPercentage: 70
  targetMemoryUtilizationPercentage: 80

# Anti-affinity for high availability
affinity:
  podAntiAffinity:
    preferredDuringSchedulingIgnoredDuringExecution:
    - weight: 100
      podAffinityTerm:
        labelSelector:
          matchExpressions:
          - key: app.kubernetes.io/name
            operator: In
            values:
            - matey
        topologyKey: kubernetes.io/hostname

# Pod disruption budget
podDisruptionBudget:
  enabled: true
  minAvailable: 3

postgresql:
  enabled: true
  architecture: replication
  auth:
    username: matey
    password: secure-production-password
    database: matey_prod
  primary:
    persistence:
      enabled: true
      size: 50Gi
      storageClass: "fast-ssd"
  readReplicas:
    replicaCount: 2
    persistence:
      enabled: true
      size: 50Gi
      storageClass: "fast-ssd"

# Production features
config:
  debug: false
  log_level: warn
  metrics_enabled: true
  tracing_enabled: true
  audit_enabled: true
  rate_limiting: true
  
# Security settings
security:
  podSecurityContext:
    runAsNonRoot: true
    runAsUser: 1000
    fsGroup: 2000
  containerSecurityContext:
    allowPrivilegeEscalation: false
    readOnlyRootFilesystem: true
    capabilities:
      drop:
      - ALL
```

## Advanced Configuration

### External Database

```yaml
# values-external-db.yaml
postgresql:
  enabled: false

externalDatabase:
  host: postgres.example.com
  port: 5432
  user: matey
  password: ""
  existingSecret: matey-db-secret
  existingSecretPasswordKey: password
  database: matey
  sslmode: require
```

### High Availability with Multiple Zones

```yaml
# values-ha.yaml
replicaCount: 6

affinity:
  podAntiAffinity:
    preferredDuringSchedulingIgnoredDuringExecution:
    - weight: 100
      podAffinityTerm:
        labelSelector:
          matchExpressions:
          - key: app.kubernetes.io/name
            operator: In
            values:
            - matey
        topologyKey: kubernetes.io/hostname
    - weight: 50
      podAffinityTerm:
        labelSelector:
          matchExpressions:
          - key: app.kubernetes.io/name
            operator: In
            values:
            - matey
        topologyKey: failure-domain.beta.kubernetes.io/zone

nodeSelector:
  kubernetes.io/arch: amd64

tolerations:
- key: "node.kubernetes.io/not-ready"
  operator: "Exists"
  effect: "NoExecute"
  tolerationSeconds: 300
- key: "node.kubernetes.io/unreachable"
  operator: "Exists"
  effect: "NoExecute"
  tolerationSeconds: 300

topologySpreadConstraints:
- maxSkew: 1
  topologyKey: failure-domain.beta.kubernetes.io/zone
  whenUnsatisfiable: DoNotSchedule
  labelSelector:
    matchLabels:
      app.kubernetes.io/name: matey
```

### Monitoring and Observability

```yaml
# values-monitoring.yaml
monitoring:
  enabled: true
  
  serviceMonitor:
    enabled: true
    namespace: monitoring
    interval: 30s
    scrapeTimeout: 10s
    labels:
      release: prometheus
      
  prometheusRule:
    enabled: true
    namespace: monitoring
    labels:
      release: prometheus
    rules:
    - alert: MateyDown
      expr: up{job="matey"} == 0
      for: 5m
      labels:
        severity: critical
      annotations:
        summary: "Matey instance is down"
        description: "Matey instance {{ $labels.instance }} has been down for more than 5 minutes."
        
    - alert: MateyHighCPU
      expr: rate(container_cpu_usage_seconds_total{pod=~"matey-.*"}[5m]) > 0.8
      for: 10m
      labels:
        severity: warning
      annotations:
        summary: "High CPU usage on Matey"
        description: "CPU usage is above 80% for more than 10 minutes."

grafana:
  enabled: true
  dashboards:
    enabled: true
    label: grafana_dashboard
    labelValue: "1"
```

### Service Mesh Integration

```yaml
# values-istio.yaml
istio:
  enabled: true
  
  virtualService:
    enabled: true
    hosts:
    - matey.example.com
    gateways:
    - matey-gateway
    
  gateway:
    enabled: true
    hosts:
    - matey.example.com
    tls:
      mode: SIMPLE
      credentialName: matey-tls
      
  destinationRule:
    enabled: true
    trafficPolicy:
      tls:
        mode: ISTIO_MUTUAL
      connectionPool:
        tcp:
          maxConnections: 100
        http:
          http1MaxPendingRequests: 50
          maxRequestsPerConnection: 10
          
  peerAuthentication:
    enabled: true
    mtls:
      mode: STRICT
      
  authorizationPolicy:
    enabled: true
    rules:
    - from:
      - source:
          principals: ["cluster.local/ns/default/sa/matey-client"]
      to:
      - operation:
          methods: ["GET", "POST"]
```

## Deployment Commands

### Install Commands

```bash
# Development deployment
helm install matey matey/matey \
  -n matey-dev \
  --create-namespace \
  -f values-dev.yaml

# Staging deployment
helm install matey matey/matey \
  -n matey-staging \
  --create-namespace \
  -f values-staging.yaml

# Production deployment
helm install matey matey/matey \
  -n matey-prod \
  --create-namespace \
  -f values-prod.yaml
```

### Upgrade Commands

```bash
# Upgrade with new values
helm upgrade matey matey/matey \
  -n matey-prod \
  -f values-prod.yaml

# Upgrade with set values
helm upgrade matey matey/matey \
  -n matey-prod \
  --set image.tag=0.0.5 \
  --set replicaCount=10

# Upgrade with timeout
helm upgrade matey matey/matey \
  -n matey-prod \
  -f values-prod.yaml \
  --timeout 600s
```

### Rollback Commands

```bash
# List releases
helm history matey -n matey-prod

# Rollback to previous version
helm rollback matey -n matey-prod

# Rollback to specific revision
helm rollback matey 3 -n matey-prod
```

## Troubleshooting

### Common Issues

#### 1. Image Pull Errors

```bash
# Check image pull secrets
kubectl get pods -n matey-system -o wide

# Create image pull secret
kubectl create secret docker-registry regcred \
  --docker-server=registry.example.com \
  --docker-username=user \
  --docker-password=pass \
  --docker-email=email@example.com

# Update values to use secret
# values.yaml
imagePullSecrets:
  - name: regcred
```

#### 2. Database Connection Issues

```bash
# Check PostgreSQL pods
kubectl get pods -n matey-system -l app=postgresql

# Check database secret
kubectl get secret postgresql-secret -n matey-system -o yaml

# Test connection
kubectl run postgres-test --image=postgres:15 --rm -i --tty -- \
  psql -h postgresql -U matey -d matey
```

#### 3. Ingress Issues

```bash
# Check ingress status
kubectl get ingress -n matey-system

# Check ingress controller
kubectl get pods -n ingress-nginx

# Check TLS certificates
kubectl get certificates -n matey-system
kubectl describe certificate matey-tls -n matey-system
```

### Debug Commands

```bash
# Get all resources
kubectl get all -n matey-system

# Check pod logs
kubectl logs -n matey-system -l app.kubernetes.io/name=matey

# Check events
kubectl get events -n matey-system --sort-by=.metadata.creationTimestamp

# Debug pod
kubectl describe pod -n matey-system -l app.kubernetes.io/name=matey

# Check helm status
helm status matey -n matey-system

# Get rendered templates
helm get manifest matey -n matey-system
```

## CI/CD Integration

### GitHub Actions

```yaml
# .github/workflows/deploy.yaml
name: Deploy Matey
on:
  push:
    branches: [main]
    
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v3
    
    - name: Setup Helm
      uses: azure/setup-helm@v3
      with:
        version: '3.10.0'
        
    - name: Configure kubectl
      uses: azure/k8s-set-context@v3
      with:
        kubeconfig: ${{ secrets.KUBE_CONFIG }}
        
    - name: Add Helm repo
      run: |
        helm repo add matey https://phildougherty.github.io/m8e
        helm repo update
        
    - name: Deploy to staging
      run: |
        helm upgrade --install matey matey/matey \
          -n matey-staging \
          --create-namespace \
          -f values-staging.yaml \
          --wait
          
    - name: Test deployment
      run: |
        kubectl rollout status deployment/matey -n matey-staging
        kubectl get pods -n matey-staging
        
    - name: Deploy to production
      if: github.ref == 'refs/heads/main'
      run: |
        helm upgrade --install matey matey/matey \
          -n matey-prod \
          --create-namespace \
          -f values-prod.yaml \
          --wait
```

### GitLab CI

```yaml
# .gitlab-ci.yml
stages:
  - deploy

deploy-staging:
  stage: deploy
  image: alpine/helm:3.10.0
  script:
    - helm repo add matey https://phildougherty.github.io/m8e
    - helm repo update
    - helm upgrade --install matey matey/matey
        -n matey-staging
        --create-namespace
        -f values-staging.yaml
        --wait
  only:
    - main
    
deploy-production:
  stage: deploy
  image: alpine/helm:3.10.0
  script:
    - helm repo add matey https://phildougherty.github.io/m8e
    - helm repo update
    - helm upgrade --install matey matey/matey
        -n matey-prod
        --create-namespace
        -f values-prod.yaml
        --wait
  only:
    - main
  when: manual
```

## Custom Chart Development

### Creating Custom Chart

```bash
# Create new chart
helm create matey-custom

# Update Chart.yaml
cat > matey-custom/Chart.yaml << EOF
apiVersion: v2
name: matey-custom
description: Custom Matey deployment
version: 0.1.0
appVersion: "0.0.4"
dependencies:
- name: matey
  version: "0.0.4"
  repository: https://phildougherty.github.io/m8e
EOF

# Update values.yaml with custom values
cat > matey-custom/values.yaml << EOF
matey:
  replicaCount: 3
  image:
    tag: "0.0.4"
  service:
    type: LoadBalancer
  ingress:
    enabled: true
    hosts:
      - host: matey.custom.com
        paths:
          - path: /
            pathType: Prefix
            
# Custom resources
customConfig:
  enabled: true
  config:
    custom_feature: true
EOF

# Install custom chart
helm install matey-custom ./matey-custom
```

This completes the Helm deployment guide. For more advanced scenarios, see the [Kubernetes deployment guide](kubernetes.md) and [security configuration](security.md).