// internal/mcp/path_resolver.go
package mcp

import (
	"os"
	"path/filepath"
	"strings"
)

// PathMapping represents a path mapping between host and container
type PathMapping struct {
	HostPath      string `json:"host_path"`
	ContainerPath string `json:"container_path"`
	ServerName    string `json:"server_name"`
}

// PathResolver handles path translation between different MCP server contexts
type PathResolver struct {
	// Host working directory (where user started)
	HostWorkingDir string
	
	// Container working directory (for current MCP server)
	ContainerWorkingDir string
	
	// Path mappings for different MCP servers
	mappings []PathMapping
}

// NewPathResolver creates a new path resolver with dynamic mappings
func NewPathResolver() *PathResolver {
	// Try to get the user's original working directory from environment
	hostWorkingDir := os.Getenv("MATEY_HOST_PWD")
	if hostWorkingDir == "" {
		// Fallback to current working directory
		hostWorkingDir, _ = os.Getwd()
	}

	// Get container working directory
	containerWorkingDir, _ := os.Getwd()

	// Determine user's home directory dynamically
	homeDir := os.Getenv("HOME")
	if homeDir == "" {
		homeDir = "/home/" + os.Getenv("USER")
		if homeDir == "/home/" {
			homeDir = "/tmp" // Ultimate fallback
		}
	}

	return &PathResolver{
		HostWorkingDir:      hostWorkingDir,
		ContainerWorkingDir: containerWorkingDir,
		mappings: []PathMapping{
			{
				HostPath:      homeDir,
				ContainerPath: "/workspace",
				ServerName:    "filesystem",
			},
			{
				HostPath:      homeDir,
				ContainerPath: "/projects",
				ServerName:    "playwright",
			},
			{
				HostPath:      "/tmp",
				ContainerPath: "/tmp",
				ServerName:    "filesystem",
			},
		},
	}
}

// ResolveHostToContainer converts a host path to container path for a specific server
func (pr *PathResolver) ResolveHostToContainer(hostPath, serverName string) string {
	// If it's already an absolute path that looks like a container path, return as-is
	if strings.HasPrefix(hostPath, "/workspace") || strings.HasPrefix(hostPath, "/projects") {
		return hostPath
	}

	// Handle relative paths
	if !filepath.IsAbs(hostPath) {
		hostPath = filepath.Join(pr.HostWorkingDir, hostPath)
	}

	// Find appropriate mapping
	for _, mapping := range pr.mappings {
		if mapping.ServerName == serverName && strings.HasPrefix(hostPath, mapping.HostPath) {
			// Convert host path to container path
			relativePath := strings.TrimPrefix(hostPath, mapping.HostPath)
			return filepath.Join(mapping.ContainerPath, relativePath)
		}
	}

	// Default fallback - assume it's already a valid path
	return hostPath
}

// ResolveContainerToHost converts a container path to host path
func (pr *PathResolver) ResolveContainerToHost(containerPath string) string {
	// Find appropriate mapping
	for _, mapping := range pr.mappings {
		if strings.HasPrefix(containerPath, mapping.ContainerPath) {
			// Convert container path to host path
			relativePath := strings.TrimPrefix(containerPath, mapping.ContainerPath)
			return filepath.Join(mapping.HostPath, relativePath)
		}
	}

	// If no mapping found, assume it's already a host path
	return containerPath
}

// GetCurrentWorkingDir returns the appropriate working directory for path resolution
func (pr *PathResolver) GetCurrentWorkingDir() string {
	return pr.HostWorkingDir
}

// ResolveWorkingDir resolves a working directory based on context
func (pr *PathResolver) ResolveWorkingDir(workingDir string) string {
	if workingDir == "" {
		return pr.HostWorkingDir
	}

	// If it's a relative path, make it relative to host working dir
	if !filepath.IsAbs(workingDir) {
		return filepath.Join(pr.HostWorkingDir, workingDir)
	}

	return workingDir
}

// GetContextInfo returns context information for the AI
func (pr *PathResolver) GetContextInfo() map[string]interface{} {
	// Find home directory mapping for context
	var homeDirMapping PathMapping
	for _, mapping := range pr.mappings {
		if mapping.ServerName == "filesystem" && mapping.ContainerPath == "/workspace" {
			homeDirMapping = mapping
			break
		}
	}

	return map[string]interface{}{
		"current_working_directory": pr.HostWorkingDir,
		"container_working_directory": pr.ContainerWorkingDir,
		"path_mappings": map[string]interface{}{
			"filesystem_server": map[string]string{
				"host_root":      homeDirMapping.HostPath,
				"container_root": "/workspace",
				"description":    "File operations (read_file, edit_file) use /workspace mapping",
			},
			"playwright_server": map[string]string{
				"host_root":      homeDirMapping.HostPath, 
				"container_root": "/projects",
				"description":    "Browser automation uses /projects mapping",
			},
		},
		"guidance": map[string]interface{}{
			"relative_paths": "Use relative paths from current working directory (" + pr.HostWorkingDir + ")",
			"parent_navigation": "Use '../' to navigate to parent directories",
			"current_directory_files": "Use './' or just filename for files in current directory",  
			"examples": map[string]string{
				"current_dir_file":  "README.md or ./README.md",
				"parent_dir_file":   "../other-project/file.go",
				"sibling_dir_file":  "./internal/mcp/server.go",
				"absolute_path":     pr.HostWorkingDir + "/some/file.txt",
			},
		},
		"important_notes": []string{
			"I am currently in: " + pr.HostWorkingDir,
			"Relative paths are resolved from this working directory",
			"All file operations support both relative and absolute paths",
			"Use 'ls' or file exploration tools to understand directory structure",
			"Home directory (" + homeDirMapping.HostPath + ") is mapped to /workspace in filesystem operations",
		},
	}
}

// AddMapping adds a custom path mapping
func (pr *PathResolver) AddMapping(hostPath, containerPath, serverName string) {
	pr.mappings = append(pr.mappings, PathMapping{
		HostPath:      hostPath,
		ContainerPath: containerPath,
		ServerName:    serverName,
	})
}