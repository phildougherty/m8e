# Security Policy

## Overview

Matey (m8e) is a Kubernetes-native MCP server orchestrator that treats security as a design priority. This document outlines our security practices, vulnerability reporting process, and security guidelines.

> **Maturity note:** Matey is pre-1.0 software (v0.0.4, ~40% feature-complete). Several security-related features described below are implemented and usable, while others are partial or aspirational. Each section is marked accordingly. Do not assume compliance certifications or production-grade guarantees — see the "Compliance & Standards" section for what is aspirational versus real.

## Supported Versions

We provide security updates for the following versions:

| Version | Supported          | Status |
| ------- | ------------------ | ------ |
| 0.0.4   | :white_check_mark: | Current stable release |
| 0.0.3   | :white_check_mark: | Security patches only |
| < 0.0.3 | :x:                | Not supported |

## Security Features

Each item is labelled with its current maturity: **[stable]** works today, **[experimental]** implemented but not battle-tested, **[partial]** incomplete, **[aspirational]** planned but not yet implemented.

### Authentication & Authorization
- **OAuth 2.1 Implementation** *[experimental]*: OAuth 2.1 issuer with PKCE support (`internal/auth/oauth.go`). Covered by unit tests; token revocation lacks integration-test coverage.
- **JWT Token Management** *[experimental]*: Token generation and validation work. Revocation is implemented but untested end-to-end.
- **RBAC Integration** *[partial]*: Controllers generate least-privilege ServiceAccounts and Roles for deployed MCP servers. Auth middleware is not yet wired into the Kubernetes controllers as a request gate.
- **API Key Authentication** *[stable]*: Fallback authentication method with scope validation.
- **Middleware Chains** *[experimental]*: Layered authentication middleware exists for the proxy.

### Container & Kubernetes Security
- **Security Contexts** *[stable]*: Non-root user execution, dropped capabilities, read-only filesystems are supported in server specs.
- **RBAC Policies** *[partial]*: Least-privilege ServiceAccounts are generated for deployed servers; cluster-wide RBAC story is still being completed.
- **Network Policies** *[aspirational]*: Network policy templates are part of the new Helm chart; not yet validated across environments.
- **Pod Security Standards** *[partial]*: Generated workloads follow Pod Security Standards conventions but are not formally validated.
- **Image Security** *[stable]*: Multi-stage builds and minimal base images.

### Data Protection
- **Encryption in Transit** *[partial]*: TLS is supported for proxy communications when configured; not enforced by default.
- **Secrets Management** *[stable]*: Integration with Kubernetes Secrets. External secret store integration is not built in.
- **Audit Logging** *[partial — in-memory only]*: Audit events are recorded **in memory only**. File and database storage backends are not implemented and fall back to memory; audit history is lost on restart. Logs are **not** tamper-evident or persisted. Treat audit logging as a development aid, not a compliance control.
- **Data Isolation** *[stable]*: All Matey components run in the `matey` namespace; namespace-based isolation applies.

### Supply Chain Security
- **Dependency Scanning** *[stable]*: Automated dependency vulnerability checks in CI.
- **Image Scanning** *[aspirational]*: Container image scanning is planned.
- **Signed Releases** *[aspirational]*: Release artifact signing is planned, not yet in place.
- **SBOM Generation** *[aspirational]*: SBOM generation is planned.

## Security Scanning

We aim to employ automated security scanning across the toolchain. The tools below represent the intended scanning posture; not all are wired into CI yet. Check `.github/` for what is currently active.

### Static Analysis
- **gosec**: Go-specific security vulnerability scanner
- **golangci-lint**: Code quality and security linting
- **semgrep**: Pattern-based static analysis *(aspirational)*

### Dependency Scanning
- **govulncheck**: Official Go vulnerability database integration
- **Dependabot**: Automated dependency updates with security patches
- **nancy**: OSS Index dependency vulnerability scanning *(aspirational)*

### Container Security *(aspirational)*
- **Trivy**: Container image vulnerability and misconfiguration scanning
- **Docker Scout**: Docker image security analysis
- **Snyk**: Third-party security vulnerability scanning

### Infrastructure Scanning *(aspirational)*
- **Kubernetes CIS Benchmarks**: Security configuration validation
- **Network Policy Validation**: Traffic flow security analysis
- **RBAC Analysis**: Permission escalation detection

## Vulnerability Reporting

### Reporting Security Issues

We take security vulnerabilities seriously. If you discover a security vulnerability, please report it responsibly:

#### For Security Vulnerabilities
**DO NOT** create public GitHub issues for security vulnerabilities.

Instead, please email us at: **security@matey-orchestrator.com**

Include the following information:
- Description of the vulnerability
- Steps to reproduce the issue
- Potential impact assessment
- Any suggested fixes or mitigations
- Your contact information for follow-up

#### Response Timeline
- **Initial Response**: Within 24 hours of report
- **Vulnerability Assessment**: Within 72 hours
- **Fix Development**: Within 7-14 days depending on severity
- **Patch Release**: Critical issues within 24-48 hours
- **Public Disclosure**: 30 days after patch release (coordinated disclosure)

#### Severity Classification

| Severity | Description | Response Time |
|----------|-------------|---------------|
| **Critical** | Remote code execution, privilege escalation | 24-48 hours |
| **High** | Information disclosure, DoS attacks | 7 days |
| **Medium** | Limited scope vulnerabilities | 14 days |
| **Low** | Minor security issues | 30 days |

### Bug Bounty Program

While we don't currently have a formal bug bounty program, we recognize and appreciate security researchers who help improve Matey's security:

- **Hall of Fame**: Public recognition for responsible disclosure
- **Swag & Merchandise**: Matey branded items for valid reports
- **Early Access**: Preview access to new features and releases
- **Direct Communication**: Access to development team for collaboration

## Security Best Practices

### For Users

#### Deployment Security
```bash
# Use security contexts
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
spec:
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    fsGroup: 2000
  containers:
  - name: matey
    securityContext:
      allowPrivilegeEscalation: false
      readOnlyRootFilesystem: true
      capabilities:
        drop:
        - ALL
EOF
```

#### Network Security
```bash
# Apply network policies
kubectl apply -f charts/matey/templates/network-policy.yaml

# Use TLS for all communications
export MATEY_TLS_ENABLED=true
export MATEY_TLS_CERT_FILE=/etc/certs/tls.crt
export MATEY_TLS_KEY_FILE=/etc/certs/tls.key
```

#### Secrets Management
```bash
# Use Kubernetes secrets
kubectl create secret generic matey-secrets \
  --from-literal=api-key="$(openssl rand -base64 32)" \
  --from-literal=jwt-secret="$(openssl rand -base64 64)"

# Or use external secret management
helm install external-secrets external-secrets/external-secrets
```

#### Resource Limits
```yaml
# Set resource limits
resources:
  limits:
    cpu: 500m
    memory: 512Mi
  requests:
    cpu: 100m
    memory: 128Mi
```

### For Developers

#### Secure Coding Guidelines
1. **Input Validation**: Always validate and sanitize user inputs
2. **Error Handling**: Don't expose sensitive information in error messages
3. **Logging**: Avoid logging sensitive data (passwords, tokens, keys)
4. **Dependencies**: Keep dependencies updated and scan for vulnerabilities
5. **Authentication**: Use strong authentication mechanisms and secure session management

#### Pre-commit Security Checks
```bash
# Install pre-commit hooks
pre-commit install

# Run security scans
make security-scan

# Check for secrets
git secrets --scan

# Lint for security issues
golangci-lint run --enable gosec
```

#### Code Review Checklist
- [ ] No hardcoded secrets or credentials
- [ ] Proper input validation and sanitization
- [ ] Secure error handling without information disclosure
- [ ] RBAC permissions follow least-privilege principle
- [ ] TLS/encryption used for sensitive data transmission
- [ ] Dependencies are up-to-date and vulnerability-free
- [ ] Security tests cover the new functionality

## Compliance & Standards

> **Important:** Matey has **not** been audited or certified against any compliance standard or regulatory framework. The items below are reference frameworks we design toward — they are **aspirational goals**, not claims of compliance. Do not deploy Matey in environments with regulatory requirements (HIPAA, PCI DSS, SOC 2, etc.) and assume those requirements are met. They are not.

### Standards We Design Toward (aspirational)
- **CIS Kubernetes Benchmark**: Container and Kubernetes security guidelines
- **NIST Cybersecurity Framework**: Risk management and security controls
- **OWASP Top 10**: Application security best practices

### Regulatory Frameworks (aspirational — not implemented or certified)
- **GDPR**, **SOX**, **HIPAA**, **PCI DSS**: Matey provides no certified controls for any of these. Achieving compliance would require substantial additional configuration, controls, and third-party audit that Matey does not currently support.

## Security Monitoring

### Metrics & Alerting
```yaml
# Example Prometheus alerts
groups:
- name: matey-security
  rules:
  - alert: UnauthorizedAccess
    expr: increase(matey_auth_failures_total[5m]) > 10
    for: 2m
    labels:
      severity: critical
    annotations:
      summary: "High number of authentication failures"

  - alert: SuspiciousActivity
    expr: increase(matey_api_requests_total{status=~"4..|5.."}[10m]) > 100
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: "High number of HTTP errors"
```

### Log Analysis
```bash
# Security-focused log queries
kubectl logs -l app=matey | grep -E "(failed|error|unauthorized|forbidden)"
```

> Note: Matey's built-in audit logging is currently in-memory only and not exported to a queryable file or store. For durable audit trails today, rely on Kubernetes' own audit logging and your cluster's log aggregation.

## Incident Response

### Response Team
- **Security Lead**: Primary security contact and incident commander
- **Development Team**: Technical analysis and fix implementation
- **DevOps Team**: Infrastructure and deployment response
- **Communications**: External communication and user notifications

### Response Process
1. **Detection**: Automated monitoring alerts or external reports
2. **Assessment**: Severity analysis and impact determination
3. **Containment**: Immediate steps to limit exposure
4. **Investigation**: Root cause analysis and evidence collection
5. **Remediation**: Fix development and deployment
6. **Recovery**: Service restoration and monitoring
7. **Lessons Learned**: Post-incident review and improvements

### Communication Plan
- **Internal**: Slack channel #security-incidents
- **External**: Security advisory via GitHub and email
- **Users**: Status page updates and migration guidance
- **Community**: Public disclosure after fixes are available

## Contact Information

- **Security Team**: security@matey-orchestrator.com
- **General Support**: support@matey-orchestrator.com
- **Project Maintainer**: [@phildougherty](https://github.com/phildougherty)
- **Security Advisory**: Watch this repository for security advisories

## Resources

- [OWASP Kubernetes Security Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Kubernetes_Security_Cheat_Sheet.html)
- [CIS Kubernetes Benchmark](https://www.cisecurity.org/benchmark/kubernetes)
- [Kubernetes Security Best Practices](https://kubernetes.io/docs/concepts/security/)
- [Go Security Guidelines](https://github.com/OWASP/Go-SCP)

---

**Last Updated**: May 2026  
**Applies to**: v0.0.4