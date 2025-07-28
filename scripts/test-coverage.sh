#!/bin/bash
# Comprehensive test coverage script for Matey
# Provides detailed coverage analysis with reporting and quality gates

set -euo pipefail

# Configuration
COVERAGE_DIR="coverage"
COVERAGE_THRESHOLD=${COVERAGE_THRESHOLD:-60}
COVERAGE_MODE="atomic"
TIMEOUT="10m"
PARALLEL_LEVEL=4

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

# Clean up function
cleanup() {
    if [ -f "${COVERAGE_DIR}/tmp.profile" ]; then
        rm -f "${COVERAGE_DIR}/tmp.profile"
    fi
}
trap cleanup EXIT

# Main function
main() {
    log_info "Starting comprehensive test coverage analysis for Matey"
    
    # Create coverage directory
    mkdir -p "${COVERAGE_DIR}"
    
    # Initialize coverage profile
    echo "mode: ${COVERAGE_MODE}" > "${COVERAGE_DIR}/coverage.out"
    
    # Get all packages with tests
    log_info "Discovering test packages..."
    packages=$(go list ./... | grep -v vendor)
    test_packages=()
    
    for pkg in $packages; do
        if go list -f '{{.TestGoFiles}}' "$pkg" | grep -q '.go'; then
            test_packages+=("$pkg")
        fi
    done
    
    if [ ${#test_packages[@]} -eq 0 ]; then
        log_error "No test packages found"
        exit 1
    fi
    
    log_info "Found ${#test_packages[@]} packages with tests"
    
    # Run tests with coverage for each package
    log_info "Running tests with coverage..."
    failed_packages=()
    
    for pkg in "${test_packages[@]}"; do
        pkg_name=$(echo "$pkg" | sed 's|github.com/phildougherty/m8e/||')
        log_info "Testing package: $pkg_name"
        
        if go test -timeout="${TIMEOUT}" -race -covermode="${COVERAGE_MODE}" \
           -coverprofile="${COVERAGE_DIR}/tmp.profile" \
           -parallel="${PARALLEL_LEVEL}" "$pkg"; then
            
            # Append to main coverage file (skip mode line)
            if [ -f "${COVERAGE_DIR}/tmp.profile" ]; then
                tail -n +2 "${COVERAGE_DIR}/tmp.profile" >> "${COVERAGE_DIR}/coverage.out"
                rm -f "${COVERAGE_DIR}/tmp.profile"
            fi
        else
            log_error "Tests failed for package: $pkg_name"
            failed_packages+=("$pkg_name")
        fi
    done
    
    # Check for test failures
    if [ ${#failed_packages[@]} -gt 0 ]; then
        log_error "Tests failed for the following packages:"
        for failed_pkg in "${failed_packages[@]}"; do
            log_error "  - $failed_pkg"
        done
        exit 1
    fi
    
    # Generate coverage reports
    log_info "Generating coverage reports..."
    
    # HTML report
    go tool cover -html="${COVERAGE_DIR}/coverage.out" -o "${COVERAGE_DIR}/coverage.html"
    log_success "HTML coverage report: ${COVERAGE_DIR}/coverage.html"
    
    # Text report
    go tool cover -func="${COVERAGE_DIR}/coverage.out" > "${COVERAGE_DIR}/coverage.txt"
    log_success "Text coverage report: ${COVERAGE_DIR}/coverage.txt"
    
    # Extract total coverage
    total_coverage=$(go tool cover -func="${COVERAGE_DIR}/coverage.out" | \
                    grep "total:" | awk '{print $3}' | sed 's/%//')
    
    if [ -z "$total_coverage" ]; then
        log_error "Could not determine total coverage"
        exit 1
    fi
    
    # Display results
    echo ""
    echo "=========================================="
    echo "           COVERAGE SUMMARY"
    echo "=========================================="
    echo "Total Coverage: ${total_coverage}%"
    echo "Coverage Threshold: ${COVERAGE_THRESHOLD}%"
    
    # Package-level coverage
    echo ""
    echo "Package Coverage Details:"
    echo "========================"
    go tool cover -func="${COVERAGE_DIR}/coverage.out" | \
        grep -v "total:" | \
        sort -k3 -nr | \
        head -20
    
    # Generate JSON report for CI/CD
    generate_json_report "$total_coverage"
    
    # Coverage threshold check
    if (( $(echo "$total_coverage >= $COVERAGE_THRESHOLD" | bc -l) )); then
        log_success "Coverage ${total_coverage}% meets threshold of ${COVERAGE_THRESHOLD}%"
        exit 0
    else
        log_error "Coverage ${total_coverage}% is below threshold of ${COVERAGE_THRESHOLD}%"
        
        # Show uncovered functions to help developers
        echo ""
        echo "Top uncovered functions:"
        echo "======================="
        go tool cover -func="${COVERAGE_DIR}/coverage.out" | \
            awk '$3 ~ /^0\.0%$/ {print $1 ":" $2}' | \
            head -10
        
        exit 1
    fi
}

# Generate JSON report for CI/CD integration
generate_json_report() {
    local total_coverage=$1
    
    cat > "${COVERAGE_DIR}/coverage.json" << EOF
{
  "coverage": {
    "total": ${total_coverage},
    "threshold": ${COVERAGE_THRESHOLD},
    "status": "$([ $(echo "$total_coverage >= $COVERAGE_THRESHOLD" | bc -l) -eq 1 ] && echo "pass" || echo "fail")",
    "timestamp": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
    "reports": {
      "html": "${COVERAGE_DIR}/coverage.html",
      "text": "${COVERAGE_DIR}/coverage.txt",
      "profile": "${COVERAGE_DIR}/coverage.out"
    }
  }
}
EOF
    
    log_success "JSON coverage report: ${COVERAGE_DIR}/coverage.json"
}

# Run integration tests separately
run_integration_tests() {
    log_info "Running integration tests..."
    
    if go test -v -tags=integration -timeout="${TIMEOUT}" \
       -covermode="${COVERAGE_MODE}" \
       -coverprofile="${COVERAGE_DIR}/integration-coverage.out" \
       ./internal/integration/...; then
        
        log_success "Integration tests passed"
        
        # Generate integration coverage report
        if [ -f "${COVERAGE_DIR}/integration-coverage.out" ]; then
            go tool cover -html="${COVERAGE_DIR}/integration-coverage.out" \
                -o "${COVERAGE_DIR}/integration-coverage.html"
            log_success "Integration coverage report: ${COVERAGE_DIR}/integration-coverage.html"
        fi
    else
        log_error "Integration tests failed"
        return 1
    fi
}

# Parse command line arguments
case "${1:-all}" in
    "all")
        main
        ;;
    "integration")
        run_integration_tests
        ;;
    "unit")
        main
        ;;
    "clean")
        log_info "Cleaning coverage files..."
        rm -rf "${COVERAGE_DIR}"
        log_success "Coverage files cleaned"
        ;;
    *)
        echo "Usage: $0 [all|unit|integration|clean]"
        echo "  all         - Run all tests with coverage (default)"
        echo "  unit        - Run unit tests with coverage only"
        echo "  integration - Run integration tests with coverage only"
        echo "  clean       - Clean coverage files"
        exit 1
        ;;
esac