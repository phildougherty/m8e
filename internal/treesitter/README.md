# Tree-Sitter Code Parsing

This package provides advanced code parsing and understanding using Tree-sitter.

## Components

### `parser.go`
- Tree-sitter Go bindings integration
- Multi-language AST parsing support:
  - Go
  - Python  
  - JavaScript/TypeScript
  - Rust
  - Java
  - C/C++
- Lazy loading of language grammars
- Parser caching for performance

### `definitions.go`
- Code definition extraction from ASTs:
  - Function definitions
  - Method definitions with receivers (Go-specific)
  - Class/struct definitions
  - Interface definitions
  - Type declarations
- Code summaries and signatures generation
- Go-specific constructs (embedding, receivers, etc.)

### `queries.go`
- Language-specific tree-sitter queries
- Query result processing and caching
- Support for:
  - Function/method extraction
  - Type declarations
  - Import statements
  - Comments and documentation
  - Language-specific patterns

## Features

- **Multi-Language Support**: Parses 6+ programming languages
- **AST-Based Analysis**: Deep understanding of code structure
- **Performance Optimized**: Lazy loading and caching
- **Go-Aware**: Special handling for Go idioms and patterns
- **Rich Queries**: Sophisticated pattern matching for code elements

## Usage

This package enables:
- AI assistants to understand code structure and context
- Intelligent code navigation and analysis  
- Extraction of function signatures and documentation
- Context-aware code suggestions and edits
- Integration with file editing for structure-aware modifications