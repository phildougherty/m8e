#!/bin/bash
# Basic security scanning script for Matey using only built-in Go tools
# This is a fallback script that doesn't require external tool installation

set -euo pipefail

# Configuration
SECURITY_DIR="security"

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

# Create security directory
mkdir -p "$SECURITY_DIR"

log_info "Starting basic security scan for Matey using built-in Go tools..."

# 1. Go vet analysis
log_info "Running go vet analysis..."
if go vet ./... > "${SECURITY_DIR}/vet.txt" 2>&1; then
    log_success "go vet completed - no issues found"
    echo "No issues found by go vet" > "${SECURITY_DIR}/vet.txt"
else
    log_warning "go vet found potential issues - check ${SECURITY_DIR}/vet.txt"
fi

# 2. Check for hardcoded secrets with grep
log_info "Scanning for potential hardcoded secrets..."
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
echo "# Potential Hardcoded Secrets Scan Results" > "${SECURITY_DIR}/secrets.txt"
echo "Generated: $(date)" >> "${SECURITY_DIR}/secrets.txt"
echo "" >> "${SECURITY_DIR}/secrets.txt"

for pattern in "${secret_patterns[@]}"; do
    if grep -r -i -E "$pattern" --include="*.go" --include="*.yaml" \
       --include="*.yml" --include="*.json" --exclude-dir="vendor" \
       --exclude-dir=".git" . >> "${SECURITY_DIR}/secrets_raw.txt" 2>/dev/null; then
        secrets_found=true
    fi
done

if [ "$secrets_found" = true ]; then
    log_warning "Potential hardcoded secrets found - review ${SECURITY_DIR}/secrets.txt"
    echo "WARNING: Potential hardcoded secrets found:" >> "${SECURITY_DIR}/secrets.txt"
    cat "${SECURITY_DIR}/secrets_raw.txt" >> "${SECURITY_DIR}/secrets.txt" 2>/dev/null || true
else
    log_success "No hardcoded secrets detected"
    echo "No potential hardcoded secrets detected." >> "${SECURITY_DIR}/secrets.txt"
fi
rm -f "${SECURITY_DIR}/secrets_raw.txt"

# 3. Check dependencies for known issues
log_info "Checking Go module dependencies..."
go list -m all > "${SECURITY_DIR}/dependencies.txt"
dep_count=$(wc -l < "${SECURITY_DIR}/dependencies.txt")
log_info "Found $dep_count dependencies"

# Check for potentially problematic patterns in dependencies
log_info "Scanning dependencies for potential issues..."
echo "# Dependency Analysis" > "${SECURITY_DIR}/dependency-analysis.txt"
echo "Generated: $(date)" >> "${SECURITY_DIR}/dependency-analysis.txt"
echo "Total dependencies: $dep_count" >> "${SECURITY_DIR}/dependency-analysis.txt"
echo "" >> "${SECURITY_DIR}/dependency-analysis.txt"

# Look for pre-release or development versions
if grep -E "v0\.0\.|pre|dev|alpha|beta|rc" "${SECURITY_DIR}/dependencies.txt" > "${SECURITY_DIR}/prerelease-deps.txt"; then
    log_warning "Found pre-release or development dependencies"
    echo "Pre-release or development dependencies found:" >> "${SECURITY_DIR}/dependency-analysis.txt"
    cat "${SECURITY_DIR}/prerelease-deps.txt" >> "${SECURITY_DIR}/dependency-analysis.txt"
    echo "" >> "${SECURITY_DIR}/dependency-analysis.txt"
else
    echo "No pre-release or development dependencies found." >> "${SECURITY_DIR}/dependency-analysis.txt"
fi
rm -f "${SECURITY_DIR}/prerelease-deps.txt"

# 4. Check for suspicious file permissions
log_info "Checking file permissions..."
echo "# File Permissions Analysis" > "${SECURITY_DIR}/permissions.txt"
echo "Generated: $(date)" >> "${SECURITY_DIR}/permissions.txt"
echo "" >> "${SECURITY_DIR}/permissions.txt"

# Find files that might have overly permissive permissions
find . -type f \( -name "*.go" -o -name "*.sh" -o -name "*.yaml" -o -name "*.yml" -o -name "*.json" \) \
    -not -path "./.git/*" -not -path "./vendor/*" \
    -perm /022 >> "${SECURITY_DIR}/permissions-raw.txt" 2>/dev/null || true

if [ -s "${SECURITY_DIR}/permissions-raw.txt" ]; then
    log_warning "Files with potentially unsafe permissions found"
    echo "Files with group/world write permissions:" >> "${SECURITY_DIR}/permissions.txt"
    while read -r file; do
        perm=$(stat -c "%a %n" "$file" 2>/dev/null || echo "unknown $file")
        echo "$perm" >> "${SECURITY_DIR}/permissions.txt"
    done < "${SECURITY_DIR}/permissions-raw.txt"
else
    log_success "File permissions look secure"
    echo "No files with unsafe permissions found." >> "${SECURITY_DIR}/permissions.txt"
fi
rm -f "${SECURITY_DIR}/permissions-raw.txt"

# 5. Check for TODO/FIXME/XXX comments that might indicate security issues
log_info "Scanning for security-related TODO/FIXME comments..."
echo "# Security-Related TODOs and FIXMEs" > "${SECURITY_DIR}/todos.txt"
echo "Generated: $(date)" >> "${SECURITY_DIR}/todos.txt"
echo "" >> "${SECURITY_DIR}/todos.txt"

security_keywords="TODO|FIXME|XXX|HACK|BUG"
security_context="security|auth|password|token|secret|vulnerable|exploit|unsafe|danger"

if grep -r -n -E "($security_keywords).*($security_context)|($security_context).*($security_keywords)" \
   --include="*.go" --exclude-dir="vendor" --exclude-dir=".git" . \
   >> "${SECURITY_DIR}/todos.txt" 2>/dev/null; then
    log_warning "Found security-related TODO/FIXME comments - review ${SECURITY_DIR}/todos.txt"
else
    echo "No security-related TODO/FIXME comments found." >> "${SECURITY_DIR}/todos.txt"
    log_success "No security-related TODOs found"
fi

# 6. Check for common Go security antipatterns
log_info "Checking for common Go security antipatterns..."
echo "# Common Go Security Antipatterns" > "${SECURITY_DIR}/antipatterns.txt"
echo "Generated: $(date)" >> "${SECURITY_DIR}/antipatterns.txt"
echo "" >> "${SECURITY_DIR}/antipatterns.txt"

antipatterns_found=false

# Check for direct shell command execution
if grep -r -n "exec.Command\|os.Exec\|syscall.Exec" --include="*.go" --exclude-dir="vendor" . \
   >> "${SECURITY_DIR}/antipatterns-raw.txt" 2>/dev/null; then
    echo "Direct shell command execution found:" >> "${SECURITY_DIR}/antipatterns.txt"
    cat "${SECURITY_DIR}/antipatterns-raw.txt" >> "${SECURITY_DIR}/antipatterns.txt"
    echo "" >> "${SECURITY_DIR}/antipatterns.txt"
    antipatterns_found=true
fi

# Check for SQL string concatenation (potential SQL injection)
if grep -r -n -E "SELECT.*\+|INSERT.*\+|UPDATE.*\+|DELETE.*\+" --include="*.go" --exclude-dir="vendor" . \
   >> "${SECURITY_DIR}/sql-concat.txt" 2>/dev/null; then
    echo "Potential SQL injection via string concatenation:" >> "${SECURITY_DIR}/antipatterns.txt"
    cat "${SECURITY_DIR}/sql-concat.txt" >> "${SECURITY_DIR}/antipatterns.txt"
    echo "" >> "${SECURITY_DIR}/antipatterns.txt"
    antipatterns_found=true
fi

# Check for use of math/rand instead of crypto/rand for security purposes
if grep -r -n "math/rand" --include="*.go" --exclude-dir="vendor" . \
   >> "${SECURITY_DIR}/weak-rand.txt" 2>/dev/null; then
    echo "Use of math/rand (consider crypto/rand for security-sensitive operations):" >> "${SECURITY_DIR}/antipatterns.txt"
    cat "${SECURITY_DIR}/weak-rand.txt" >> "${SECURITY_DIR}/antipatterns.txt"
    echo "" >> "${SECURITY_DIR}/antipatterns.txt"
    antipatterns_found=true
fi

if [ "$antipatterns_found" = true ]; then
    log_warning "Potential security antipatterns found - review ${SECURITY_DIR}/antipatterns.txt"
else
    echo "No common security antipatterns detected." >> "${SECURITY_DIR}/antipatterns.txt"
    log_success "No security antipatterns detected"
fi

# Cleanup temporary files
rm -f "${SECURITY_DIR}"/*-raw.txt "${SECURITY_DIR}"/sql-concat.txt "${SECURITY_DIR}"/weak-rand.txt

# 7. Generate summary report
log_info "Generating security summary report..."
cat > "${SECURITY_DIR}/summary.txt" << EOF
# Matey Basic Security Scan Summary

Generated: $(date)

## Scan Results

### Go Vet Analysis
$(if [ -s "${SECURITY_DIR}/vet.txt" ]; then echo "Issues found - see vet.txt for details"; else echo "No issues found"; fi)

### Hardcoded Secrets Scan
$(if grep -q "WARNING" "${SECURITY_DIR}/secrets.txt"; then echo "⚠️  Potential secrets found - review secrets.txt"; else echo "✅ No hardcoded secrets detected"; fi)

### Dependency Analysis
✅ $dep_count dependencies analyzed
$(if [ -s "${SECURITY_DIR}/prerelease-deps.txt" ] 2>/dev/null; then echo "⚠️  Pre-release dependencies found"; else echo "✅ All dependencies are stable releases"; fi)

### File Permissions
$(if grep -q "group/world write" "${SECURITY_DIR}/permissions.txt"; then echo "⚠️  Unsafe file permissions found"; else echo "✅ File permissions are secure"; fi)

### Security TODOs
$(if grep -v "^#\|^Generated:\|^$\|No security-related" "${SECURITY_DIR}/todos.txt" >/dev/null 2>&1; then echo "⚠️  Security-related TODOs found"; else echo "✅ No security-related TODOs"; fi)

### Security Antipatterns
$(if [ "$antipatterns_found" = true ]; then echo "⚠️  Potential security antipatterns found"; else echo "✅ No common antipatterns detected"; fi)

## Recommendations

1. **External Security Tools**: For comprehensive security analysis, install and run:
   - gosec: go install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest
   - govulncheck: go install golang.org/x/vuln/cmd/govulncheck@latest

2. **Regular Scanning**: Run security scans regularly, especially before releases

3. **Code Review**: Ensure security-focused code reviews for all changes

4. **Dependency Management**: Keep dependencies updated and monitor for vulnerabilities

## Files Generated

- summary.txt: This summary report
- vet.txt: Go vet analysis results
- secrets.txt: Hardcoded secrets scan results
- dependencies.txt: Complete dependency list
- dependency-analysis.txt: Dependency analysis
- permissions.txt: File permissions analysis
- todos.txt: Security-related TODO/FIXME comments
- antipatterns.txt: Common security antipattern analysis

EOF

echo ""
echo "=========================================="
echo "     BASIC SECURITY SCAN COMPLETE"
echo "=========================================="
echo ""
echo "Summary report: ${SECURITY_DIR}/summary.txt"
echo ""
echo "Key findings:"
if grep -q "⚠️" "${SECURITY_DIR}/summary.txt"; then
    log_warning "Security issues found - review the detailed reports"
    grep "⚠️" "${SECURITY_DIR}/summary.txt" | sed 's/^/  /'
    echo ""
    log_info "For comprehensive security analysis, consider installing external security tools"
    exit 1
else
    log_success "Basic security scan completed - no major issues found"
    exit 0
fi