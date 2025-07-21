#!/bin/bash
# Comprehensive security scanning script for Matey
# Provides detailed security analysis with multiple tools and reporting

set -euo pipefail

# Configuration
SECURITY_DIR="security"
SARIF_OUTPUT="${SECURITY_DIR}/combined-security.sarif"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Helper functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if a command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Install gosec if not present
install_gosec() {
    if ! command_exists gosec; then
        log_info "Installing gosec security scanner..."
        go install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest
    fi
}

# Install govulncheck if not present
install_govulncheck() {
    if ! command_exists govulncheck; then
        log_info "Installing govulncheck vulnerability scanner..."
        go install golang.org/x/vuln/cmd/govulncheck@latest
    fi
}

# Install nancy for dependency scanning
install_nancy() {
    if ! command_exists nancy; then
        log_info "Installing nancy dependency scanner..."
        go install github.com/sonatypecommunity/nancy@latest
    fi
}

# Install semgrep if available
install_semgrep() {
    if command_exists pip3; then
        if ! command_exists semgrep; then
            log_info "Installing semgrep static analysis tool..."
            pip3 install --user semgrep
        fi
    else
        log_warning "pip3 not available, skipping semgrep installation"
    fi
}

# Run gosec security scan
run_gosec() {
    log_info "Running gosec security scan..."
    
    install_gosec
    
    # Run gosec with JSON output
    if gosec -fmt json -out "${SECURITY_DIR}/gosec.json" \
       -stdout -verbose=text ./...; then
        log_success "gosec scan completed"
    else
        log_warning "gosec found security issues"
    fi
    
    # Also generate SARIF format for GitHub integration
    gosec -fmt sarif -out "${SECURITY_DIR}/gosec.sarif" ./... || true
    
    # Generate human-readable report
    gosec -fmt text -out "${SECURITY_DIR}/gosec.txt" ./... || true
}

# Run vulnerability check
run_govulncheck() {
    log_info "Running govulncheck vulnerability scan..."
    
    install_govulncheck
    
    # Run vulnerability check with JSON output
    if govulncheck -json ./... > "${SECURITY_DIR}/govulncheck.json"; then
        log_success "govulncheck scan completed - no vulnerabilities found"
    else
        log_warning "govulncheck found vulnerabilities"
        
        # Generate human-readable report
        govulncheck ./... > "${SECURITY_DIR}/govulncheck.txt" || true
    fi
}

# Run dependency audit with nancy
run_nancy() {
    log_info "Running nancy dependency audit..."
    
    install_nancy
    
    # Generate go.list for nancy
    go list -json -m all > "${SECURITY_DIR}/go.list"
    
    # Run nancy audit
    if nancy sleuth -f "${SECURITY_DIR}/go.list" \
       -o json > "${SECURITY_DIR}/nancy.json"; then
        log_success "nancy audit completed - no vulnerable dependencies found"
    else
        log_warning "nancy found vulnerable dependencies"
        
        # Generate human-readable report
        nancy sleuth -f "${SECURITY_DIR}/go.list" > "${SECURITY_DIR}/nancy.txt" || true
    fi
}

# Run semgrep analysis
run_semgrep() {
    if command_exists semgrep; then
        log_info "Running semgrep static analysis..."
        
        # Run semgrep with Go rules
        if semgrep --config=auto --json \
           --output="${SECURITY_DIR}/semgrep.json" .; then
            log_success "semgrep analysis completed"
        else
            log_warning "semgrep found potential issues"
        fi
        
        # Generate SARIF format
        semgrep --config=auto --sarif \
            --output="${SECURITY_DIR}/semgrep.sarif" . || true
    else
        log_info "semgrep not available, skipping static analysis"
    fi
}

# Check for hardcoded secrets
check_secrets() {
    log_info "Scanning for hardcoded secrets..."
    
    # Common secret patterns
    secret_patterns=(
        "password.*=.*['\"][^'\"]{8,}['\"]"
        "secret.*=.*['\"][^'\"]{16,}['\"]"
        "token.*=.*['\"][^'\"]{20,}['\"]"
        "key.*=.*['\"][^'\"]{20,}['\"]"
        "api[_-]?key.*=.*['\"][^'\"]{20,}['\"]"
        "auth[_-]?token.*=.*['\"][^'\"]{20,}['\"]"
        "private[_-]?key.*=.*['\"][^'\"]{20,}['\"]"
        "access[_-]?token.*=.*['\"][^'\"]{20,}['\"]"
    )
    
    secrets_found=false
    
    for pattern in "${secret_patterns[@]}"; do
        if grep -r -i -E "$pattern" --include="*.go" --include="*.yaml" \
           --include="*.yml" --include="*.json" . > /dev/null 2>&1; then
            echo "Potential secret found with pattern: $pattern" >> "${SECURITY_DIR}/secrets.txt"
            secrets_found=true
        fi
    done
    
    if [ "$secrets_found" = true ]; then
        log_warning "Potential hardcoded secrets found - check ${SECURITY_DIR}/secrets.txt"
    else
        log_success "No hardcoded secrets detected"
        echo "No hardcoded secrets detected" > "${SECURITY_DIR}/secrets.txt"
    fi
}

# Analyze file permissions
check_permissions() {
    log_info "Checking file permissions..."
    
    # Find files with overly permissive permissions
    find . -type f \( -name "*.go" -o -name "*.sh" -o -name "*.yaml" -o -name "*.yml" \) \
        -perm /022 > "${SECURITY_DIR}/permissions.txt" 2>/dev/null || true
    
    if [ -s "${SECURITY_DIR}/permissions.txt" ]; then
        log_warning "Files with overly permissive permissions found"
    else
        log_success "File permissions look secure"
        echo "No overly permissive files found" > "${SECURITY_DIR}/permissions.txt"
    fi
}

# Generate comprehensive security report
generate_security_report() {
    log_info "Generating comprehensive security report..."
    
    cat > "${SECURITY_DIR}/security-report.md" << EOF
# Matey Security Analysis Report

Generated on: $(date -u +"%Y-%m-%d %H:%M:%S UTC")

## Summary

This report provides a comprehensive security analysis of the Matey codebase using multiple security scanning tools.

## Tools Used

- **gosec**: Go security checker for common security issues
- **govulncheck**: Official Go vulnerability database scanner
- **nancy**: Dependency vulnerability scanner using OSS Index
- **semgrep**: Static analysis tool for finding bugs and security issues
- **custom scripts**: Hardcoded secrets detection and permission analysis

## Results

### gosec Results
$(if [ -f "${SECURITY_DIR}/gosec.txt" ]; then cat "${SECURITY_DIR}/gosec.txt"; else echo "No gosec report available"; fi)

### govulncheck Results
$(if [ -f "${SECURITY_DIR}/govulncheck.txt" ]; then cat "${SECURITY_DIR}/govulncheck.txt"; else echo "No vulnerability report available"; fi)

### nancy Results
$(if [ -f "${SECURITY_DIR}/nancy.txt" ]; then cat "${SECURITY_DIR}/nancy.txt"; else echo "No dependency audit report available"; fi)

### Secrets Scan Results
$(cat "${SECURITY_DIR}/secrets.txt")

### File Permissions Analysis
$(cat "${SECURITY_DIR}/permissions.txt")

## Recommendations

1. **Regular Scanning**: Run this security scan regularly, especially before releases
2. **Dependency Updates**: Keep dependencies updated to latest secure versions
3. **Code Review**: Ensure all code changes go through security-focused code review
4. **Secret Management**: Use external secret management systems for production deployments
5. **RBAC**: Implement proper RBAC controls in Kubernetes deployments

## Next Steps

- Review and address any high-severity issues found
- Implement automated security scanning in CI/CD pipeline
- Consider additional security measures like runtime security monitoring
- Regular penetration testing for production deployments
EOF
    
    log_success "Security report generated: ${SECURITY_DIR}/security-report.md"
}

# Generate JSON summary for CI/CD integration
generate_json_summary() {
    local gosec_issues=0
    local vuln_issues=0
    local nancy_issues=0
    local secrets_issues=0
    local permission_issues=0
    
    # Count issues from each tool
    if [ -f "${SECURITY_DIR}/gosec.json" ]; then
        gosec_issues=$(jq '.Stats.found // 0' "${SECURITY_DIR}/gosec.json" 2>/dev/null || echo 0)
    fi
    
    if [ -f "${SECURITY_DIR}/govulncheck.json" ]; then
        vuln_issues=$(jq '[.Vulns // []] | length' "${SECURITY_DIR}/govulncheck.json" 2>/dev/null || echo 0)
    fi
    
    if [ -f "${SECURITY_DIR}/nancy.json" ]; then
        nancy_issues=$(jq '[.vulnerable // []] | length' "${SECURITY_DIR}/nancy.json" 2>/dev/null || echo 0)
    fi
    
    if [ -f "${SECURITY_DIR}/secrets.txt" ] && ! grep -q "No hardcoded secrets" "${SECURITY_DIR}/secrets.txt"; then
        secrets_issues=1
    fi
    
    if [ -f "${SECURITY_DIR}/permissions.txt" ] && ! grep -q "No overly permissive" "${SECURITY_DIR}/permissions.txt"; then
        permission_issues=1
    fi
    
    local total_issues=$((gosec_issues + vuln_issues + nancy_issues + secrets_issues + permission_issues))
    
    cat > "${SECURITY_DIR}/security-summary.json" << EOF
{
  "security_scan": {
    "timestamp": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
    "total_issues": ${total_issues},
    "tools": {
      "gosec": {
        "issues": ${gosec_issues},
        "report": "gosec.json"
      },
      "govulncheck": {
        "issues": ${vuln_issues},
        "report": "govulncheck.json"
      },
      "nancy": {
        "issues": ${nancy_issues},
        "report": "nancy.json"
      },
      "secrets": {
        "issues": ${secrets_issues},
        "report": "secrets.txt"
      },
      "permissions": {
        "issues": ${permission_issues},
        "report": "permissions.txt"
      }
    },
    "status": "$([ $total_issues -eq 0 ] && echo "pass" || echo "fail")",
    "reports": {
      "comprehensive": "security-report.md",
      "summary": "security-summary.json"
    }
  }
}
EOF
    
    log_success "Security summary JSON: ${SECURITY_DIR}/security-summary.json"
}

# Main function
main() {
    log_info "Starting comprehensive security scan for Matey"
    
    # Create security directory
    mkdir -p "${SECURITY_DIR}"
    
    # Run all security scans
    run_gosec
    run_govulncheck
    run_nancy
    run_semgrep
    check_secrets
    check_permissions
    
    # Generate reports
    generate_security_report
    generate_json_summary
    
    # Final summary
    echo ""
    echo "=========================================="
    echo "        SECURITY SCAN COMPLETE"
    echo "=========================================="
    
    if [ -f "${SECURITY_DIR}/security-summary.json" ]; then
        local status=$(jq -r '.security_scan.status' "${SECURITY_DIR}/security-summary.json")
        local total_issues=$(jq -r '.security_scan.total_issues' "${SECURITY_DIR}/security-summary.json")
        
        echo "Status: $status"
        echo "Total Issues: $total_issues"
        
        if [ "$status" = "pass" ]; then
            log_success "Security scan passed - no critical issues found"
            exit 0
        else
            log_warning "Security scan found $total_issues issues - review reports"
            exit 1
        fi
    else
        log_error "Failed to generate security summary"
        exit 1
    fi
}

# Parse command line arguments
case "${1:-all}" in
    "all")
        main
        ;;
    "gosec")
        mkdir -p "${SECURITY_DIR}"
        run_gosec
        ;;
    "vulns")
        mkdir -p "${SECURITY_DIR}"
        run_govulncheck
        ;;
    "deps")
        mkdir -p "${SECURITY_DIR}"
        run_nancy
        ;;
    "secrets")
        mkdir -p "${SECURITY_DIR}"
        check_secrets
        ;;
    "clean")
        log_info "Cleaning security scan files..."
        rm -rf "${SECURITY_DIR}"
        log_success "Security scan files cleaned"
        ;;
    *)
        echo "Usage: $0 [all|gosec|vulns|deps|secrets|clean]"
        echo "  all     - Run comprehensive security scan (default)"
        echo "  gosec   - Run gosec security checker only"
        echo "  vulns   - Run vulnerability check only"
        echo "  deps    - Run dependency audit only"
        echo "  secrets - Run secrets scan only"
        echo "  clean   - Clean security scan files"
        exit 1
        ;;
esac