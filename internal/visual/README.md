# Visual Interface Components

This package provides rich terminal-based visual interfaces for file editing and diff viewing.

## Components

### `diff_view.go`
- Rich terminal diff display with syntax highlighting
- Multiple diff modes:
  - Side-by-side diff mode
  - Unified diff mode
- Interactive navigation controls
- Approval/rejection interface for AI edits
- Integration with existing Bubble Tea UI

### `file_view.go`
- Interactive file tree navigation
- Fuzzy search integration for quick file finding
- Preview pane with syntax highlighting
- Git status indicators
- File selection and action controls
- Keyboard shortcuts and navigation

### `editor_view.go`
- Interactive file editing interface
- Real-time diff preview during editing
- Undo/redo capabilities
- Search and replace functionality
- Integration with tree-sitter for syntax awareness

## Features

- **Rich Terminal UI**: Leverages Bubble Tea for smooth interactions
- **Syntax Highlighting**: Code-aware display using existing glamour integration
- **Interactive Navigation**: Keyboard-driven interface design
- **Real-time Preview**: Live diff updates during editing
- **Git Integration**: Visual indicators for version control status

## Usage

This package provides:
- Visual diff approval interface for AI-suggested edits
- Interactive file browser for context exploration
- Rich editing experience within the terminal
- Integration with chat interface for seamless workflow
- Keyboard-driven productivity features