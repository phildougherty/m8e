# File Editing System

This package provides advanced file editing capabilities for Matey, including:

## Components

### `diff.go`
- SEARCH/REPLACE format parser inspired by Cline
- Multi-level fallback matching strategies:
  - Exact string matching
  - Line-trimmed fallback matching  
  - Block anchor matching (first/last line anchors)
- Streaming diff application
- Out-of-order replacement support

### `editor.go`
- File reading with encoding detection
- Multi-edit operations (MultiEdit functionality)
- Backup and rollback capabilities
- Kubernetes ConfigMaps integration for persistence
- File validation and safety checks

### `fallback.go`
- Smart matching algorithms for when exact string matches fail
- Whitespace-tolerant line matching
- Block anchor matching strategy
- Fuzzy context matching as last resort
- Matching confidence scoring

## Features

- **Streaming Edits**: Real-time diff application as AI generates content
- **Smart Fallbacks**: Multiple strategies to handle text mismatches
- **Kubernetes Native**: Integrates with existing K8s infrastructure
- **Safety First**: Comprehensive validation and rollback capabilities

## Usage

This package will be used by:
- MCP tools for file editing
- CLI commands for interactive editing
- Chat interface for AI-driven file modifications