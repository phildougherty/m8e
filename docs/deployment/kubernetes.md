# Kubernetes Deployment Guide

This guide covers deploying Matey in production Kubernetes environments.

## Prerequisites

- Kubernetes cluster (1.28+)
- `kubectl` configured with cluster admin permissions
- Helm 3.0+ (for Helm deployment)
- Persistent storage provisioner configured

## Deployment Methods

### Method 1: Helm Deployment (Recommended)

#### Install Matey Chart

```bash
# Add the Matey Helm repository
helm repo add matey https://phildougherty.github.io/m8e
helm repo update

# Create namespace
kubectl create namespace matey-system

# Install with default values
helm install matey matey/matey -n matey-system

# Or install with custom values
helm install matey matey/matey -n matey-system -f values.yaml
```

#### Custom Values Configuration

Create a `values.yaml` file:

```yaml
# values.yaml
replicaCount: 3

image:
  repository: phildougherty/matey
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
    cpu: 500m
    memory: 512Mi
  requests:
    cpu: 250m
    memory: 256Mi

persistence:
  enabled: true
  storageClass: "fast-ssd"
  size: 10Gi

rbac:
  create: true
  
serviceAccount:
  create: true
  name: matey-controller

# Database configuration
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

# Monitoring
monitoring:
  enabled: true
  prometheus:
    enabled: true
  grafana:
    enabled: true
    adminPassword: admin-password
```

### Method 2: Manual Kubernetes Deployment

#### 1. Create Namespace

```bash
kubectl create namespace matey-system
```

#### 2. Install CRDs

```bash
kubectl apply -f https://raw.githubusercontent.com/phildougherty/m8e/main/config/crd/
```

#### 3. Create Service Account and RBAC

```yaml
# rbac.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: matey-controller
  namespace: matey-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: matey-controller
rules:
- apiGroups: [""]
  resources: ["pods", "services", "endpoints", "configmaps", "secrets"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: ["apps"]
  resources: ["deployments", "replicasets"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: ["matey.io"]
  resources: ["mcpservers", "mcpmemories", "mcpproxies", "mcptaskschedulers", "workflows"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: ["apiextensions.k8s.io"]
  resources: ["customresourcedefinitions"]
  verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: matey-controller
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: matey-controller
subjects:
- kind: ServiceAccount
  name: matey-controller
  namespace: matey-system
```

#### 4. Deploy Controller

```yaml
# controller.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: matey-controller
  namespace: matey-system
spec:
  replicas: 3
  selector:
    matchLabels:
      app: matey-controller
  template:
    metadata:
      labels:
        app: matey-controller
    spec:
      serviceAccountName: matey-controller
      containers:
      - name: controller
        image: phildougherty/matey:0.0.4
        command: ["matey", "controller"]
        env:
        - name: NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
        - name: POD_NAME
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        resources:
          limits:
            cpu: 500m
            memory: 512Mi
          requests:
            cpu: 250m
            memory: 256Mi
        livenessProbe:
          httpGet:
            path: /healthz
            port: 8081
          initialDelaySeconds: 15
          periodSeconds: 20
        readinessProbe:
          httpGet:
            path: /readyz
            port: 8081
          initialDelaySeconds: 5
          periodSeconds: 10
---
apiVersion: v1
kind: Service
metadata:
  name: matey-controller
  namespace: matey-system
spec:
  selector:
    app: matey-controller
  ports:
  - port: 8080
    targetPort: 8080
    name: http
  - port: 8081
    targetPort: 8081
    name: health
```

#### 5. Apply Manifests

```bash
kubectl apply -f rbac.yaml
kubectl apply -f controller.yaml
```

### Method 3: Operator Lifecycle Manager (OLM)

```bash
# Install OLM if not present
curl -sL https://github.com/operator-framework/operator-lifecycle-manager/releases/download/v0.25.0/install.sh | bash -s v0.25.0

# Create catalog source
kubectl apply -f - <<EOF
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: matey-catalog
  namespace: olm
spec:
  sourceType: grpc
  image: quay.io/phildougherty/matey-catalog:latest
  displayName: Matey Operators
  publisher: Matey
EOF

# Install operator
kubectl apply -f - <<EOF
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: matey
  namespace: matey-system
spec:
  channel: stable
  name: matey
  source: matey-catalog
  sourceNamespace: olm
EOF
```

## Production Configuration

### High Availability Setup

```yaml
# ha-values.yaml
replicaCount: 3

# Anti-affinity rules
affinity:
  podAntiAffinity:
    preferredDuringSchedulingIgnoredDuringExecution:
    - weight: 100
      podAffinityTerm:
        labelSelector:
          matchExpressions:
          - key: app
            operator: In
            values:
            - matey-controller
        topologyKey: kubernetes.io/hostname

# Pod disruption budget
podDisruptionBudget:
  enabled: true
  minAvailable: 2

# Resource limits
resources:
  limits:
    cpu: 1000m
    memory: 1Gi
  requests:
    cpu: 500m
    memory: 512Mi

# Horizontal Pod Autoscaler
autoscaling:
  enabled: true
  minReplicas: 3
  maxReplicas: 10
  targetCPUUtilizationPercentage: 70
  targetMemoryUtilizationPercentage: 80
```

### Database Configuration

#### PostgreSQL with High Availability

```yaml
# postgresql-ha.yaml
postgresql:
  enabled: true
  architecture: replication
  auth:
    username: matey
    password: secure-password
    database: matey
  primary:
    resources:
      limits:
        cpu: 1000m
        memory: 1Gi
      requests:
        cpu: 500m
        memory: 512Mi
    persistence:
      enabled: true
      size: 50Gi
      storageClass: "fast-ssd"
  readReplicas:
    replicaCount: 2
    resources:
      limits:
        cpu: 500m
        memory: 512Mi
      requests:
        cpu: 250m
        memory: 256Mi
    persistence:
      enabled: true
      size: 50Gi
      storageClass: "fast-ssd"
  metrics:
    enabled: true
```

#### External Database

```yaml
# external-db.yaml
postgresql:
  enabled: false

externalDatabase:
  host: postgres.example.com
  port: 5432
  user: matey
  password: secure-password
  database: matey
  sslmode: require
  
  # Connection pooling
  pooling:
    enabled: true
    maxConnections: 100
    minConnections: 5
```

### Security Configuration

#### Network Policies

```yaml
# network-policy.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: matey-network-policy
  namespace: matey-system
spec:
  podSelector:
    matchLabels:
      app: matey-controller
  policyTypes:
  - Ingress
  - Egress
  ingress:
  - from:
    - namespaceSelector:
        matchLabels:
          name: default
    - podSelector:
        matchLabels:
          app: matey-client
    ports:
    - protocol: TCP
      port: 8080
  egress:
  - to:
    - namespaceSelector:
        matchLabels:
          name: kube-system
    ports:
    - protocol: TCP
      port: 443
  - to:
    - podSelector:
        matchLabels:
          app: postgresql
    ports:
    - protocol: TCP
      port: 5432
```

#### Pod Security Standards

```yaml
# pod-security.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: matey-system
  labels:
    pod-security.kubernetes.io/enforce: restricted
    pod-security.kubernetes.io/audit: restricted
    pod-security.kubernetes.io/warn: restricted
```

#### Service Mesh Integration

```yaml
# istio-config.yaml
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: matey-mtls
  namespace: matey-system
spec:
  mtls:
    mode: STRICT
---
apiVersion: security.istio.io/v1beta1
kind: AuthorizationPolicy
metadata:
  name: matey-authz
  namespace: matey-system
spec:
  selector:
    matchLabels:
      app: matey-controller
  rules:
  - from:
    - source:
        principals: ["cluster.local/ns/default/sa/matey-client"]
    to:
    - operation:
        methods: ["GET", "POST"]
```

## Monitoring and Observability

### Prometheus Configuration

```yaml
# prometheus-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: prometheus-config
  namespace: matey-system
data:
  prometheus.yml: |
    global:
      scrape_interval: 15s
    scrape_configs:
    - job_name: 'matey-controller'
      static_configs:
      - targets: ['matey-controller:8080']
      metrics_path: /metrics
      scrape_interval: 15s
    - job_name: 'matey-services'
      kubernetes_sd_configs:
      - role: pod
        namespaces:
          names:
          - default
      relabel_configs:
      - source_labels: [__meta_kubernetes_pod_label_app]
        action: keep
        regex: mcp-server
```

### Grafana Dashboards

```yaml
# grafana-dashboard.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: matey-dashboard
  namespace: matey-system
  labels:
    grafana_dashboard: "1"
data:
  matey-overview.json: |
    {
      "dashboard": {
        "title": "Matey Overview",
        "panels": [
          {
            "title": "MCP Server Status",
            "type": "stat",
            "targets": [
              {
                "expr": "sum(matey_mcp_server_status{status=\"running\"})"
              }
            ]
          },
          {
            "title": "Request Rate",
            "type": "graph",
            "targets": [
              {
                "expr": "rate(matey_http_requests_total[5m])"
              }
            ]
          }
        ]
      }
    }
```

### Logging Configuration

```yaml
# logging-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: fluent-bit-config
  namespace: matey-system
data:
  fluent-bit.conf: |
    [SERVICE]
        Flush         1
        Log_Level     info
        Daemon        off
        Parsers_File  parsers.conf
        
    [INPUT]
        Name              tail
        Path              /var/log/containers/*matey*.log
        Parser            docker
        Tag               matey.*
        
    [OUTPUT]
        Name              forward
        Match             matey.*
        Host              elasticsearch
        Port              9200
        Index             matey-logs
```

## Backup and Recovery

### Database Backup

```yaml
# backup-cronjob.yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: matey-db-backup
  namespace: matey-system
spec:
  schedule: "0 2 * * *"
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: backup
            image: postgres:15
            command:
            - /bin/bash
            - -c
            - |
              pg_dump -h $POSTGRES_HOST -U $POSTGRES_USER -d $POSTGRES_DB | \
              gzip > /backup/matey-$(date +%Y%m%d-%H%M%S).sql.gz
            env:
            - name: POSTGRES_HOST
              value: "postgresql"
            - name: POSTGRES_USER
              value: "matey"
            - name: POSTGRES_DB
              value: "matey"
            - name: PGPASSWORD
              valueFrom:
                secretKeyRef:
                  name: postgresql-secret
                  key: password
            volumeMounts:
            - name: backup-storage
              mountPath: /backup
          volumes:
          - name: backup-storage
            persistentVolumeClaim:
              claimName: backup-pvc
          restartPolicy: OnFailure
```

### Configuration Backup

```bash
# Backup script
#!/bin/bash
DATE=$(date +%Y%m%d-%H%M%S)
BACKUP_DIR="/backup/matey-config-$DATE"

# Create backup directory
mkdir -p $BACKUP_DIR

# Backup CRDs
kubectl get crd -o yaml | grep -A 1000 "matey.io" > $BACKUP_DIR/crds.yaml

# Backup custom resources
kubectl get mcpservers -A -o yaml > $BACKUP_DIR/mcpservers.yaml
kubectl get mcpmemories -A -o yaml > $BACKUP_DIR/mcpmemories.yaml
kubectl get mcpproxies -A -o yaml > $BACKUP_DIR/mcpproxies.yaml
kubectl get workflows -A -o yaml > $BACKUP_DIR/workflows.yaml

# Backup secrets
kubectl get secrets -n matey-system -o yaml > $BACKUP_DIR/secrets.yaml

# Backup configmaps
kubectl get configmaps -n matey-system -o yaml > $BACKUP_DIR/configmaps.yaml

# Compress backup
tar -czf /backup/matey-config-$DATE.tar.gz $BACKUP_DIR
rm -rf $BACKUP_DIR
```

## Disaster Recovery

### Recovery Procedures

#### 1. Restore Database

```bash
# Restore from backup
kubectl run postgres-restore --image=postgres:15 --rm -i --tty -- \
  psql -h postgresql -U matey -d matey < /backup/matey-20240115-020000.sql
```

#### 2. Restore Configuration

```bash
# Restore CRDs
kubectl apply -f crds.yaml

# Restore custom resources
kubectl apply -f mcpservers.yaml
kubectl apply -f mcpmemories.yaml
kubectl apply -f mcpproxies.yaml
kubectl apply -f workflows.yaml

# Restore secrets (decrypt if necessary)
kubectl apply -f secrets.yaml

# Restore configmaps
kubectl apply -f configmaps.yaml
```

#### 3. Verify Recovery

```bash
# Check controller status
kubectl get pods -n matey-system

# Check custom resources
kubectl get mcpservers -A
kubectl get workflows -A

# Test functionality
matey ps
matey validate
```

## Troubleshooting

### Common Issues

#### 1. CRD Installation Problems

```bash
# Check CRD status
kubectl get crd | grep matey

# Describe CRD for issues
kubectl describe crd mcpservers.matey.io

# Check controller logs
kubectl logs -n matey-system deployment/matey-controller
```

#### 2. RBAC Issues

```bash
# Check service account
kubectl get sa -n matey-system

# Check cluster role binding
kubectl describe clusterrolebinding matey-controller

# Test permissions
kubectl auth can-i create pods --as=system:serviceaccount:matey-system:matey-controller
```

#### 3. Resource Issues

```bash
# Check resource usage
kubectl top pods -n matey-system

# Check resource limits
kubectl describe pod -n matey-system -l app=matey-controller

# Check node resources
kubectl describe nodes
```

### Health Checks

```bash
# Controller health
kubectl get pods -n matey-system -l app=matey-controller

# Service health
kubectl get svc -n matey-system

# Endpoint health
kubectl get endpoints -n matey-system

# Custom resource health
kubectl get mcpservers -A
kubectl describe mcpserver <name> -n <namespace>
```

## Upgrades

### Helm Upgrade

```bash
# Update repository
helm repo update

# Upgrade with same values
helm upgrade matey matey/matey -n matey-system

# Upgrade with new values
helm upgrade matey matey/matey -n matey-system -f new-values.yaml

# Rollback if needed
helm rollback matey 1 -n matey-system
```

### Manual Upgrade

```bash
# Update CRDs
kubectl apply -f https://raw.githubusercontent.com/phildougherty/m8e/main/config/crd/

# Update controller image
kubectl set image deployment/matey-controller controller=phildougherty/matey:0.0.5 -n matey-system

# Check rollout status
kubectl rollout status deployment/matey-controller -n matey-system
```

This completes the Kubernetes deployment guide. For additional deployment scenarios, see the [Helm deployment guide](helm.md) and [security best practices](security.md).