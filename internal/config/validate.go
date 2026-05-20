// internal/config/validate.go
package config

import (
	"fmt"
	"os"
	"sort"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

// ValidationError is a single configuration problem mapped back to its
// location in the source file. Line is 1-based; 0 means "location unknown".
type ValidationError struct {
	File    string
	Line    int
	Path    string // dotted config path, e.g. `servers.foo.image`
	Message string
}

func (e ValidationError) Error() string {
	loc := e.File
	if e.Line > 0 {
		loc = fmt.Sprintf("%s:%d", e.File, e.Line)
	}
	if e.Path != "" {
		return fmt.Sprintf("%s: %s: %s", loc, e.Path, e.Message)
	}

	return fmt.Sprintf("%s: %s", loc, e.Message)
}

// ValidationErrors is a collection of validation problems. It implements error
// so the whole batch can be returned as a single value while still exposing
// each individual problem to callers that want to render them separately.
type ValidationErrors []ValidationError

func (errs ValidationErrors) Error() string {
	if len(errs) == 0 {

		return ""
	}
	parts := make([]string, len(errs))
	for i, e := range errs {
		parts[i] = e.Error()
	}

	return strings.Join(parts, "\n")
}

// lineIndex maps dotted config paths to their line number in the source YAML.
// It is built once by walking the decoded yaml.Node tree so semantic
// validation errors (which only know a config path) can be located in the file.
type lineIndex struct {
	file  string
	paths map[string]int
}

// lookup returns the best-known line for a config path, walking up parent
// segments when the exact path is not indexed (e.g. an error on a field of a
// struct that decoded into a scalar map entry).
func (li *lineIndex) lookup(path string) int {
	if li == nil {

		return 0
	}
	if line, ok := li.paths[path]; ok {

		return line
	}
	for path != "" {
		idx := strings.LastIndex(path, ".")
		if idx < 0 {
			path = ""
		} else {
			path = path[:idx]
		}
		if line, ok := li.paths[path]; ok {

			return line
		}
	}

	return 0
}

// buildLineIndex walks a YAML document node and records the line of every
// mapping key and sequence element, keyed by dotted path.
func buildLineIndex(file string, root *yaml.Node) *lineIndex {
	li := &lineIndex{file: file, paths: make(map[string]int)}
	if root == nil {

		return li
	}
	doc := root
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		doc = doc.Content[0]
	}
	indexNode(li, "", doc)

	return li
}

func indexNode(li *lineIndex, prefix string, node *yaml.Node) {
	switch node.Kind {
	case yaml.MappingNode:
		// Content alternates key, value, key, value...
		for i := 0; i+1 < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			valNode := node.Content[i+1]
			path := keyNode.Value
			if prefix != "" {
				path = prefix + "." + keyNode.Value
			}
			li.paths[path] = keyNode.Line
			indexNode(li, path, valNode)
		}
	case yaml.SequenceNode:
		for i, child := range node.Content {
			path := fmt.Sprintf("%s[%d]", prefix, i)
			li.paths[path] = child.Line
			indexNode(li, path, child)
		}
	}
}

// ValidateFile loads, parses and validates a config file, returning every
// problem found mapped to its source line. On success it also returns the
// fully-parsed config. The returned error, when non-nil, is a ValidationErrors.
func ValidateFile(filePath string) (*ComposeConfig, error) {
	loadDotEnv(filePath)

	data, err := os.ReadFile(filePath)
	if err != nil {

		return nil, ValidationErrors{{File: filePath, Message: fmt.Sprintf("failed to read config file: %v", err)}}
	}

	expandedData := os.ExpandEnv(string(data))

	// Decode once into a node tree to build the path->line index. This also
	// catches pure syntax errors with their line numbers intact.
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(expandedData), &root); err != nil {

		return nil, yamlErrorsToValidation(filePath, err)
	}
	li := buildLineIndex(filePath, &root)

	// Decode into the typed struct. Type mismatches (a string where an int is
	// expected, unknown fields, etc.) surface here as *yaml.TypeError, whose
	// messages already carry "line N" prefixes.
	var cfg ComposeConfig
	if err := root.Decode(&cfg); err != nil {

		return nil, yamlErrorsToValidation(filePath, err)
	}

	envName := os.Getenv("MCP_ENV")
	if envName == "" {
		envName = "development"
	}
	cfg.CurrentEnv = envName
	if envConfig, exists := cfg.Environments[envName]; exists {
		applyEnvironmentOverrides(&cfg, envConfig)
	}

	if errs := collectValidationErrors(&cfg, li); len(errs) > 0 {

		return nil, errs
	}

	return &cfg, nil
}

// yamlErrorsToValidation converts yaml.v3 parse/type errors into
// ValidationErrors, extracting the line numbers yaml.v3 embeds in its messages.
func yamlErrorsToValidation(file string, err error) ValidationErrors {
	var out ValidationErrors
	if typeErr, ok := err.(*yaml.TypeError); ok {
		for _, msg := range typeErr.Errors {
			line, clean := splitYAMLLine(msg)
			out = append(out, ValidationError{File: file, Line: line, Message: clean})
		}

		return out
	}
	// Plain syntax errors: "yaml: line 12: ..." — pull the line out too.
	line, clean := splitYAMLLine(err.Error())
	out = append(out, ValidationError{File: file, Line: line, Message: clean})

	return out
}

// splitYAMLLine extracts a leading "line N:" (with or without a "yaml:" prefix)
// from a yaml.v3 error message and returns the line number plus the remainder.
func splitYAMLLine(msg string) (int, string) {
	clean := strings.TrimSpace(strings.TrimPrefix(msg, "yaml:"))
	clean = strings.TrimSpace(clean)
	if !strings.HasPrefix(clean, "line ") {

		return 0, clean
	}
	rest := clean[len("line "):]
	idx := strings.IndexByte(rest, ':')
	if idx < 0 {

		return 0, clean
	}
	var line int
	if _, err := fmt.Sscanf(rest[:idx], "%d", &line); err != nil {

		return 0, clean
	}

	return line, strings.TrimSpace(rest[idx+1:])
}

// collectValidationErrors runs the full semantic validation suite and returns
// ALL problems (not just the first), each located via the line index. It does
// not weaken any existing rule in ValidateConfig — it reuses the same checks
// and additionally detects circular dependencies.
func collectValidationErrors(cfg *ComposeConfig, li *lineIndex) ValidationErrors {
	var errs ValidationErrors
	add := func(path, msg string) {
		errs = append(errs, ValidationError{File: li.file, Line: li.lookup(path), Path: path, Message: msg})
	}

	if cfg.Version != "1" {
		add("version", fmt.Sprintf("unsupported version: %q, expected \"1\"", cfg.Version))
	}

	// Iterate servers in a stable order so reported errors are deterministic.
	names := make([]string, 0, len(cfg.Servers))
	for name := range cfg.Servers {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		server := cfg.Servers[name]
		base := "servers." + name

		if err := validateServerConfig(name, server); err != nil {
			add(base, stripServerPrefix(err.Error(), name))
		}
		for _, dep := range server.DependsOn {
			if _, exists := cfg.Servers[dep]; !exists {
				add(base+".depends_on", fmt.Sprintf("depends on undefined server %q", dep))
			}
		}
		if server.Lifecycle.HumanControl != nil {
			if err := validateHumanControlConfig(name, server.Lifecycle.HumanControl); err != nil {
				add(base+".lifecycle.human_control", stripServerPrefix(err.Error(), name))
			}
		}
		if err := validateResourcePaths(name, server.Resources); err != nil {
			add(base+".resources", stripServerPrefix(err.Error(), name))
		}
		if err := validateToolsConfig(name, server.Tools); err != nil {
			add(base+".tools", stripServerPrefix(err.Error(), name))
		}
		if err := validateSecurityConfig(name, server.Security); err != nil {
			add(base+".security", stripServerPrefix(err.Error(), name))
		}
		if err := validateResourceLimits(name, server.Deploy.Resources); err != nil {
			add(base+".deploy.resources", stripServerPrefix(err.Error(), name))
		}
	}

	// Circular depends_on chains — not caught by ValidateConfig at all.
	for _, cycle := range findDependencyCycles(cfg.Servers) {
		first := cycle[0]
		add("servers."+first+".depends_on",
			fmt.Sprintf("circular dependency detected: %s", strings.Join(cycle, " -> ")))
	}

	if err := validateGlobalConfig(cfg); err != nil {
		errs = append(errs, ValidationError{File: li.file, Line: li.lookup(globalErrorPath(err.Error())), Message: err.Error()})
	}

	return errs
}

// stripServerPrefix removes the redundant `server 'name' ` prefix the legacy
// validators add, since the dotted Path already names the server.
func stripServerPrefix(msg, name string) string {
	for _, prefix := range []string{
		fmt.Sprintf("server '%s' ", name),
		fmt.Sprintf("server '%s'", name),
	} {
		if strings.HasPrefix(msg, prefix) {

			return strings.TrimSpace(msg[len(prefix):])
		}
	}

	return msg
}

// globalErrorPath does a best-effort mapping of a global validation error
// message to a top-level config path so it can be given a line number.
func globalErrorPath(msg string) string {
	switch {
	case strings.Contains(msg, "proxy_auth"):

		return "proxy_auth"
	case strings.Contains(msg, "dashboard"):

		return "dashboard"
	case strings.Contains(msg, "oauth"):

		return "oauth"
	case strings.Contains(msg, "connection"):

		return "connections"
	default:

		return ""
	}
}

// findDependencyCycles returns every dependency cycle among servers. Each
// returned slice lists the nodes of one cycle in order, with the entry node
// repeated at the end for readability.
func findDependencyCycles(servers map[string]ServerConfig) [][]string {
	const (
		white = 0 // unvisited
		gray  = 1 // on the current DFS stack
		black = 2 // fully explored
	)
	color := make(map[string]int, len(servers))
	var cycles [][]string
	seen := make(map[string]bool)

	var stack []string
	var dfs func(node string)
	dfs = func(node string) {
		color[node] = gray
		stack = append(stack, node)
		for _, dep := range servers[node].DependsOn {
			if _, exists := servers[dep]; !exists {

				continue // undefined dep reported separately
			}
			switch color[dep] {
			case white:
				dfs(dep)
			case gray:
				// Found a back edge — extract the cycle from the stack.
				start := 0
				for i, n := range stack {
					if n == dep {
						start = i

						break
					}
				}
				cycle := append([]string{}, stack[start:]...)
				cycle = append(cycle, dep)
				key := strings.Join(cycle, ",")
				if !seen[key] {
					seen[key] = true
					cycles = append(cycles, cycle)
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[node] = black
	}

	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if color[name] == white {
			dfs(name)
		}
	}

	return cycles
}
