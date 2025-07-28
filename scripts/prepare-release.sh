#!/bin/bash
# Release preparation script for Matey
# Automates release preparation with comprehensive checks and validation

set -euo pipefail

# Configuration
RELEASE_DIR="release"
CURRENT_VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "v0.0.4")
DEFAULT_NEXT_VERSION=""

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

# Check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Validate semantic version format
validate_version() {
    local version=$1
    if [[ ! $version =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9]+(\.[a-zA-Z0-9]+)*)?(\+[a-zA-Z0-9]+(\.[a-zA-Z0-9]+)*)?$ ]]; then
        log_error "Invalid semantic version format: $version"
        log_info "Expected format: vMAJOR.MINOR.PATCH[-prerelease][+build]"
        log_info "Examples: v1.0.0, v1.0.0-alpha.1, v1.0.0+build.1"
        return 1
    fi
    return 0
}

# Get next version suggestion
suggest_next_version() {
    local current=$1
    
    # Extract version without 'v' prefix
    local version_num=${current#v}
    
    # Split into major.minor.patch
    IFS='.' read -ra VERSION_PARTS <<< "$version_num"
    local major=${VERSION_PARTS[0]}
    local minor=${VERSION_PARTS[1]}
    local patch=${VERSION_PARTS[2]%%[-+]*}
    
    # Suggest patch, minor, and major bumps
    local patch_bump="v${major}.${minor}.$((patch + 1))"
    local minor_bump="v${major}.$((minor + 1)).0"
    local major_bump="v$((major + 1)).0.0"
    
    echo "Suggested versions based on current version $current:"
    echo "  Patch release (bug fixes): $patch_bump"
    echo "  Minor release (new features): $minor_bump"
    echo "  Major release (breaking changes): $major_bump"
    echo ""
    
    DEFAULT_NEXT_VERSION=$patch_bump
}

# Pre-release checks
run_pre_release_checks() {
    log_info "Running pre-release checks..."
    
    # Check Git status
    if ! git diff-index --quiet HEAD --; then
        log_error "Working directory is not clean. Please commit or stash changes."
        return 1
    fi
    
    # Check if on main branch
    local current_branch=$(git rev-parse --abbrev-ref HEAD)
    if [ "$current_branch" != "main" ]; then
        log_warning "Not on main branch (current: $current_branch). Releases should typically be made from main."
        read -p "Continue anyway? (y/N): " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            return 1
        fi
    fi
    
    # Check if up to date with remote
    git fetch origin >/dev/null 2>&1
    local local_commit=$(git rev-parse HEAD)
    local remote_commit=$(git rev-parse origin/main 2>/dev/null || echo "")
    
    if [ -n "$remote_commit" ] && [ "$local_commit" != "$remote_commit" ]; then
        log_error "Local branch is not up to date with origin/main"
        log_info "Run: git pull origin main"
        return 1
    fi
    
    log_success "Pre-release checks passed"
    return 0
}

# Run comprehensive tests
run_comprehensive_tests() {
    log_info "Running comprehensive test suite..."
    
    # Run all quality checks
    if ! make quality; then
        log_error "Quality checks failed"
        return 1
    fi
    
    # Run security scan
    if ! make security-scan; then
        log_warning "Security scan found issues - review security report"
        read -p "Continue with release despite security issues? (y/N): " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            return 1
        fi
    fi
    
    log_success "Comprehensive tests completed"
    return 0
}

# Generate changelog
generate_changelog() {
    local new_version=$1
    local previous_version=$2
    
    log_info "Generating changelog for $new_version..."
    
    mkdir -p "$RELEASE_DIR"
    
    # Get commits since last release
    local commit_range=""
    if [ "$previous_version" != "v0.0.0" ] && git rev-parse "$previous_version" >/dev/null 2>&1; then
        commit_range="${previous_version}..HEAD"
    else
        commit_range="HEAD"
    fi
    
    cat > "${RELEASE_DIR}/CHANGELOG-${new_version}.md" << EOF
# Changelog for ${new_version}

Release Date: $(date +"%Y-%m-%d")

## What's Changed

### Features
$(git log $commit_range --oneline --grep="feat:" --grep="feature:" | sed 's/^/- /')

### Bug Fixes
$(git log $commit_range --oneline --grep="fix:" --grep="bug:" | sed 's/^/- /')

### Documentation
$(git log $commit_range --oneline --grep="docs:" --grep="doc:" | sed 's/^/- /')

### Performance
$(git log $commit_range --oneline --grep="perf:" --grep="performance:" | sed 's/^/- /')

### Other Changes
$(git log $commit_range --oneline --invert-grep --grep="feat:" --grep="feature:" --grep="fix:" --grep="bug:" --grep="docs:" --grep="doc:" --grep="perf:" --grep="performance:" | sed 's/^/- /')

## Installation

### Binary Installation

Download the appropriate binary for your platform from the [releases page](https://github.com/phildougherty/m8e/releases/tag/${new_version}).

### Docker Installation

\`\`\`bash
docker pull ghcr.io/phildougherty/m8e:${new_version}
\`\`\`

### Kubernetes Installation

\`\`\`bash
# Download and install
wget https://github.com/phildougherty/m8e/releases/download/${new_version}/matey-linux-amd64.tar.gz
tar -xzf matey-linux-amd64.tar.gz
sudo mv matey /usr/local/bin/

# Install CRDs and controllers
matey install
\`\`\`

### Helm Installation

\`\`\`bash
helm repo add matey https://phildougherty.github.io/m8e
helm install matey matey/matey --version ${new_version#v}
\`\`\`

## Verification

All binaries are signed and checksums are provided:

\`\`\`bash
# Verify checksum
sha256sum -c checksums.txt
\`\`\`

## Breaking Changes

$(if git log $commit_range --oneline --grep="BREAKING CHANGE" | grep -q .; then
    echo "**This release contains breaking changes:**"
    git log $commit_range --oneline --grep="BREAKING CHANGE" | sed 's/^/- /'
    echo ""
    echo "Please review the migration guide below."
else
    echo "No breaking changes in this release."
fi)

## Migration Guide

$(if git log $commit_range --oneline --grep="BREAKING CHANGE" | grep -q .; then
    echo "### Upgrading from Previous Versions"
    echo ""
    echo "1. **Backup your configuration**: Always backup your \`matey.yaml\` configuration before upgrading"
    echo "2. **Review breaking changes**: Check the breaking changes section above"
    echo "3. **Update CRDs**: Run \`matey install\` to update Custom Resource Definitions"
    echo "4. **Restart services**: Restart all Matey services after the upgrade"
    echo ""
    echo "For detailed migration instructions, see the [migration documentation](docs/migration/)."
else
    echo "This is a backward-compatible release. Simple upgrade steps:"
    echo ""
    echo "1. Replace the binary with the new version"
    echo "2. Run \`matey install\` to ensure CRDs are up to date"
    echo "3. Restart services to pick up the new version"
fi)

## Full Changelog

**Full Changelog**: https://github.com/phildougherty/m8e/compare/${previous_version}...${new_version}

## Contributors

$(git log $commit_range --format='%aN <%aE>' | sort -u | sed 's/^/- @/')

## Security

This release has been scanned for security vulnerabilities. For security advisories, see our [security policy](SECURITY.md).

EOF

    log_success "Changelog generated: ${RELEASE_DIR}/CHANGELOG-${new_version}.md"
}

# Build release artifacts
build_release_artifacts() {
    local version=$1
    
    log_info "Building release artifacts for $version..."
    
    # Clean previous builds
    make clean
    
    # Build all targets
    if ! make release; then
        log_error "Release build failed"
        return 1
    fi
    
    # Build Docker image
    if ! make docker-build DOCKER_TAG="$version"; then
        log_error "Docker build failed"
        return 1
    fi
    
    log_success "Release artifacts built successfully"
}

# Create Git tag
create_git_tag() {
    local version=$1
    local changelog_file="${RELEASE_DIR}/CHANGELOG-${version}.md"
    
    log_info "Creating Git tag $version..."
    
    # Create annotated tag with changelog
    if [ -f "$changelog_file" ]; then
        git tag -a "$version" -F "$changelog_file"
    else
        git tag -a "$version" -m "Release $version"
    fi
    
    log_success "Git tag $version created"
}

# Validate release readiness
validate_release_readiness() {
    local version=$1
    
    log_info "Validating release readiness for $version..."
    
    # Check if tag already exists
    if git rev-parse "$version" >/dev/null 2>&1; then
        log_error "Tag $version already exists"
        return 1
    fi
    
    # Check if version is in correct format
    if ! validate_version "$version"; then
        return 1
    fi
    
    # Check if we have proper access to create releases
    if ! command_exists gh; then
        log_warning "GitHub CLI (gh) not found. Manual release creation will be required."
    fi
    
    log_success "Release validation completed"
    return 0
}

# Interactive release process
interactive_release() {
    echo "=========================================="
    echo "         MATEY RELEASE PREPARATION"
    echo "=========================================="
    echo ""
    
    log_info "Current version: $CURRENT_VERSION"
    suggest_next_version "$CURRENT_VERSION"
    
    # Get new version from user
    read -p "Enter new version [$DEFAULT_NEXT_VERSION]: " new_version
    new_version=${new_version:-$DEFAULT_NEXT_VERSION}
    
    # Validate version
    if ! validate_release_readiness "$new_version"; then
        return 1
    fi
    
    echo ""
    echo "Release Summary:"
    echo "  Current Version: $CURRENT_VERSION"
    echo "  New Version: $new_version"
    echo "  Release Branch: $(git rev-parse --abbrev-ref HEAD)"
    echo "  Release Commit: $(git rev-parse --short HEAD)"
    echo ""
    
    read -p "Proceed with release preparation? (y/N): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        log_info "Release preparation cancelled"
        return 0
    fi
    
    # Run pre-release checks
    if ! run_pre_release_checks; then
        return 1
    fi
    
    # Run comprehensive tests
    if ! run_comprehensive_tests; then
        return 1
    fi
    
    # Generate changelog
    generate_changelog "$new_version" "$CURRENT_VERSION"
    
    # Build release artifacts
    if ! build_release_artifacts "$new_version"; then
        return 1
    fi
    
    # Create Git tag
    create_git_tag "$new_version"
    
    echo ""
    echo "=========================================="
    echo "      RELEASE PREPARATION COMPLETE"
    echo "=========================================="
    echo ""
    echo "Next steps:"
    echo "1. Review the generated changelog: ${RELEASE_DIR}/CHANGELOG-${new_version}.md"
    echo "2. Push the tag to trigger release: git push origin $new_version"
    echo "3. Monitor the GitHub Actions release workflow"
    echo "4. Verify the release artifacts and Docker images"
    echo ""
    
    if command_exists gh; then
        read -p "Push tag and create GitHub release now? (y/N): " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            git push origin "$new_version"
            gh release create "$new_version" --title "Release $new_version" --notes-file "${RELEASE_DIR}/CHANGELOG-${new_version}.md"
            log_success "Release $new_version created successfully!"
        fi
    else
        log_info "Manual release creation required. Push tag with: git push origin $new_version"
    fi
}

# Main function
main() {
    case "${1:-interactive}" in
        "interactive")
            interactive_release
            ;;
        "validate")
            if [ $# -lt 2 ]; then
                log_error "Usage: $0 validate <version>"
                exit 1
            fi
            validate_release_readiness "$2"
            ;;
        "changelog")
            if [ $# -lt 2 ]; then
                log_error "Usage: $0 changelog <new_version> [previous_version]"
                exit 1
            fi
            generate_changelog "$2" "${3:-$CURRENT_VERSION}"
            ;;
        "checks")
            run_pre_release_checks && run_comprehensive_tests
            ;;
        "clean")
            log_info "Cleaning release artifacts..."
            rm -rf "$RELEASE_DIR"
            log_success "Release artifacts cleaned"
            ;;
        *)
            echo "Usage: $0 [interactive|validate|changelog|checks|clean]"
            echo "  interactive - Interactive release preparation (default)"
            echo "  validate    - Validate release readiness for a version"
            echo "  changelog   - Generate changelog for a version"
            echo "  checks      - Run pre-release checks and tests"
            echo "  clean       - Clean release artifacts"
            echo ""
            echo "Examples:"
            echo "  $0                           # Interactive release"
            echo "  $0 validate v1.0.0          # Validate v1.0.0 release"
            echo "  $0 changelog v1.0.0 v0.9.0  # Generate changelog"
            echo "  $0 checks                    # Run release checks"
            exit 1
            ;;
    esac
}

# Run main function
main "$@"