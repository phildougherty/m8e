# File Search System

This package provides high-performance file search and discovery capabilities.

## Components

### `ripgrep.go`
- Ripgrep-inspired file discovery implementation
- High-performance file traversal
- Pattern-based filtering and exclusions
- Integration with gitignore rules
- Concurrent search operations

### `fuzzy.go`
- Fuzzy filename matching using sahilm/fuzzy
- Score-based ranking of search results
- Smart relevance scoring
- Integration with file discovery
- Performance-optimized for large codebases

### `index.go`
- File indexing for fast searching
- Content-based search capabilities
- Incremental index updates
- Memory-efficient storage
- Integration with file watching

## Features

- **High Performance**: Optimized for large codebases and repositories
- **Smart Filtering**: Respects gitignore and common exclusion patterns
- **Fuzzy Search**: Intelligent matching with relevance scoring
- **Incremental Updates**: Efficient re-indexing on file changes
- **Memory Efficient**: Scales well with repository size

## Usage

This package enables:
- Fast file discovery across large projects
- Intelligent search suggestions in chat interface
- Context-aware file recommendations
- Integration with mention system for easy file referencing
- Performance-optimized search operations