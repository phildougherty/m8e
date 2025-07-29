# Context Management System

This package provides intelligent context management for Matey's AI interactions.

## Components

### `manager.go`
- Token-aware context window management
- File context tracking (reads, edits, mentions)
- Intelligent truncation strategies
- Integration with existing AI providers
- Context persistence to Kubernetes

### `search.go`
- Ripgrep-inspired file discovery
- Gitignore-aware filtering
- Fuzzy filename matching
- Directory structure analysis
- Optimization for large codebases

### `mentions.go`
- Rich @-mention system like Cline:
  - `@/path/file.ext` - file content inclusion
  - `@/path/folder/` - directory listing with file contents
  - `@problems` - Kubernetes events and pod diagnostics
  - `@logs` - recent pod logs
  - `@git-changes` - Git working directory status
- Mention parsing and validation
- Context-aware content inclusion

### `tracker.go`
- File context tracking across AI interactions
- Cross-file reference detection
- Context relevance scoring
- Automatic context cleanup

## Features

- **Smart Discovery**: Intelligent file and content discovery
- **Context Awareness**: Tracks what files and content the AI has seen
- **Mention System**: Rich @-mention syntax for easy content inclusion
- **Token Management**: Respects AI model context limits
- **Kubernetes Integration**: Persists context state in cluster

## Usage

This package enables:
- AI assistants to maintain awareness of file changes
- Smart content truncation to fit within token limits
- Easy file and content referencing via mentions
- Persistent context across chat sessions