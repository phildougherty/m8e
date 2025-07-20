package treesitter

import (
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// DefinitionType represents different types of code definitions
type DefinitionType string

const (
	DefFunction   DefinitionType = "function"
	DefMethod     DefinitionType = "method"
	DefClass      DefinitionType = "class"
	DefStruct     DefinitionType = "struct"
	DefInterface  DefinitionType = "interface"
	DefType       DefinitionType = "type"
	DefVariable   DefinitionType = "variable"
	DefConstant   DefinitionType = "constant"
	DefImport     DefinitionType = "import"
	DefPackage    DefinitionType = "package"
)

// Definition represents a code definition extracted from AST
type Definition struct {
	Type         DefinitionType `json:"type"`
	Name         string         `json:"name"`
	Signature    string         `json:"signature"`
	Summary      string         `json:"summary"`
	FilePath     string         `json:"file_path"`
	StartLine    uint32         `json:"start_line"`
	StartColumn  uint32         `json:"start_column"`
	EndLine      uint32         `json:"end_line"`
	EndColumn    uint32         `json:"end_column"`
	Language     Language       `json:"language"`
	Content      string         `json:"content"`
	DocString    string         `json:"doc_string,omitempty"`
	Parameters   []Parameter    `json:"parameters,omitempty"`
	ReturnType   string         `json:"return_type,omitempty"`
	Receiver     string         `json:"receiver,omitempty"` // For Go methods
	Package      string         `json:"package,omitempty"`
	Modifiers    []string       `json:"modifiers,omitempty"` // public, private, static, etc.
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// Parameter represents a function or method parameter
type Parameter struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Default string `json:"default,omitempty"`
}

// DefinitionExtractor extracts code definitions from AST
type DefinitionExtractor struct {
	parser   *TreeSitterParser
	language Language
	content  string
}

// NewDefinitionExtractor creates a new definition extractor
func NewDefinitionExtractor(parser *TreeSitterParser) *DefinitionExtractor {
	return &DefinitionExtractor{
		parser: parser,
	}
}

// ExtractDefinitions extracts all definitions from a parsed AST
func (de *DefinitionExtractor) ExtractDefinitions(result *ParseResult) ([]Definition, error) {
	if result.Tree == nil || result.RootNode == nil {
		return nil, fmt.Errorf("invalid parse result")
	}

	de.language = result.Language
	de.content = result.Content

	var definitions []Definition

	switch result.Language {
	case LanguageGo:
		definitions = de.extractGoDefinitions(result.RootNode)
	case LanguagePython:
		definitions = de.extractPythonDefinitions(result.RootNode)
	case LanguageJavaScript:
		definitions = de.extractJavaScriptDefinitions(result.RootNode)
	case LanguageRust:
		definitions = de.extractRustDefinitions(result.RootNode)
	default:
		return nil, fmt.Errorf("unsupported language for definition extraction: %s", result.Language)
	}

	// Add common metadata
	for i := range definitions {
		definitions[i].FilePath = result.FilePath
		definitions[i].Language = result.Language
		if definitions[i].Metadata == nil {
			definitions[i].Metadata = make(map[string]interface{})
		}
		definitions[i].Metadata["parse_time_ms"] = result.ParseTime.Milliseconds()
	}

	return definitions, nil
}

// ExtractDefinitionsByType extracts definitions of a specific type
func (de *DefinitionExtractor) ExtractDefinitionsByType(result *ParseResult, defType DefinitionType) ([]Definition, error) {
	allDefs, err := de.ExtractDefinitions(result)
	if err != nil {
		return nil, err
	}

	var filtered []Definition
	for _, def := range allDefs {
		if def.Type == defType {
			filtered = append(filtered, def)
		}
	}

	return filtered, nil
}

// FindDefinition finds a specific definition by name
func (de *DefinitionExtractor) FindDefinition(result *ParseResult, name string) (*Definition, error) {
	allDefs, err := de.ExtractDefinitions(result)
	if err != nil {
		return nil, err
	}

	for _, def := range allDefs {
		if def.Name == name {
			return &def, nil
		}
	}

	return nil, fmt.Errorf("definition not found: %s", name)
}

// Language-specific extraction methods

func (de *DefinitionExtractor) extractGoDefinitions(root *sitter.Node) []Definition {
	var definitions []Definition

	de.parser.TraverseTree(root, func(node *sitter.Node, depth int) bool {
		switch node.Type() {
		case "package_clause":
			if def := de.extractGoPackage(node); def != nil {
				definitions = append(definitions, *def)
			}
		case "import_declaration":
			defs := de.extractGoImports(node)
			definitions = append(definitions, defs...)
		case "function_declaration":
			if def := de.extractGoFunction(node); def != nil {
				definitions = append(definitions, *def)
			}
		case "method_declaration":
			if def := de.extractGoMethod(node); def != nil {
				definitions = append(definitions, *def)
			}
		case "type_declaration":
			defs := de.extractGoTypes(node)
			definitions = append(definitions, defs...)
		case "var_declaration":
			defs := de.extractGoVariables(node)
			definitions = append(definitions, defs...)
		case "const_declaration":
			defs := de.extractGoConstants(node)
			definitions = append(definitions, defs...)
		}
		return true
	})

	return definitions
}

func (de *DefinitionExtractor) extractGoPackage(node *sitter.Node) *Definition {
	nameNode := de.parser.GetChildByType(node, "package_identifier")
	if nameNode == nil {
		return nil
	}

	name := de.parser.GetNodeText(nameNode, de.content)
	startLine, startCol, endLine, endCol := de.parser.GetNodeLocation(node)

	return &Definition{
		Type:        DefPackage,
		Name:        name,
		Signature:   fmt.Sprintf("package %s", name),
		Summary:     fmt.Sprintf("Package declaration: %s", name),
		StartLine:   startLine,
		StartColumn: startCol,
		EndLine:     endLine,
		EndColumn:   endCol,
		Content:     de.parser.GetNodeText(node, de.content),
		Package:     name,
	}
}

func (de *DefinitionExtractor) extractGoImports(node *sitter.Node) []Definition {
	var definitions []Definition

	de.parser.TraverseTree(node, func(child *sitter.Node, depth int) bool {
		if child.Type() == "import_spec" {
			if def := de.extractGoImportSpec(child); def != nil {
				definitions = append(definitions, *def)
			}
		}
		return true
	})

	return definitions
}

func (de *DefinitionExtractor) extractGoImportSpec(node *sitter.Node) *Definition {
	pathNode := de.parser.GetChildByType(node, "interpreted_string_literal")
	if pathNode == nil {
		return nil
	}

	path := de.parser.GetNodeText(pathNode, de.content)
	path = strings.Trim(path, `"`) // Remove quotes

	// Check for import alias
	var alias string
	if aliasNode := de.parser.GetChildByType(node, "package_identifier"); aliasNode != nil {
		alias = de.parser.GetNodeText(aliasNode, de.content)
	}

	name := path
	if alias != "" {
		name = alias
	} else {
		// Extract package name from path
		parts := strings.Split(path, "/")
		if len(parts) > 0 {
			name = parts[len(parts)-1]
		}
	}

	startLine, startCol, endLine, endCol := de.parser.GetNodeLocation(node)

	signature := path
	if alias != "" {
		signature = fmt.Sprintf("%s %s", alias, path)
	}

	return &Definition{
		Type:        DefImport,
		Name:        name,
		Signature:   signature,
		Summary:     fmt.Sprintf("Import: %s", path),
		StartLine:   startLine,
		StartColumn: startCol,
		EndLine:     endLine,
		EndColumn:   endCol,
		Content:     de.parser.GetNodeText(node, de.content),
		Metadata: map[string]interface{}{
			"import_path": path,
			"alias":       alias,
		},
	}
}

func (de *DefinitionExtractor) extractGoFunction(node *sitter.Node) *Definition {
	nameNode := de.parser.GetChildByType(node, "identifier")
	if nameNode == nil {
		return nil
	}

	name := de.parser.GetNodeText(nameNode, de.content)
	signature := de.extractGoFunctionSignature(node)
	
	// Extract parameters
	params := de.extractGoFunctionParameters(node)
	
	// Extract return type
	returnType := de.extractGoReturnType(node)

	startLine, startCol, endLine, endCol := de.parser.GetNodeLocation(node)

	// Look for documentation comment
	docString := de.extractGoDocComment(node)

	return &Definition{
		Type:        DefFunction,
		Name:        name,
		Signature:   signature,
		Summary:     fmt.Sprintf("Function %s", name),
		StartLine:   startLine,
		StartColumn: startCol,
		EndLine:     endLine,
		EndColumn:   endCol,
		Content:     de.parser.GetNodeText(node, de.content),
		DocString:   docString,
		Parameters:  params,
		ReturnType:  returnType,
	}
}

func (de *DefinitionExtractor) extractGoMethod(node *sitter.Node) *Definition {
	nameNode := de.parser.GetChildByType(node, "field_identifier")
	if nameNode == nil {
		return nil
	}

	name := de.parser.GetNodeText(nameNode, de.content)
	signature := de.extractGoMethodSignature(node)
	
	// Extract receiver
	receiver := de.extractGoReceiver(node)
	
	// Extract parameters
	params := de.extractGoFunctionParameters(node)
	
	// Extract return type
	returnType := de.extractGoReturnType(node)

	startLine, startCol, endLine, endCol := de.parser.GetNodeLocation(node)

	// Look for documentation comment
	docString := de.extractGoDocComment(node)

	return &Definition{
		Type:        DefMethod,
		Name:        name,
		Signature:   signature,
		Summary:     fmt.Sprintf("Method %s", name),
		StartLine:   startLine,
		StartColumn: startCol,
		EndLine:     endLine,
		EndColumn:   endCol,
		Content:     de.parser.GetNodeText(node, de.content),
		DocString:   docString,
		Parameters:  params,
		ReturnType:  returnType,
		Receiver:    receiver,
	}
}

func (de *DefinitionExtractor) extractGoTypes(node *sitter.Node) []Definition {
	var definitions []Definition

	// Handle single type spec
	if typeSpec := de.parser.GetChildByType(node, "type_spec"); typeSpec != nil {
		if def := de.extractGoTypeSpec(typeSpec); def != nil {
			definitions = append(definitions, *def)
		}
		return definitions
	}

	// Handle type specs in parentheses
	de.parser.TraverseTree(node, func(child *sitter.Node, depth int) bool {
		if child.Type() == "type_spec" {
			if def := de.extractGoTypeSpec(child); def != nil {
				definitions = append(definitions, *def)
			}
		}
		return true
	})

	return definitions
}

func (de *DefinitionExtractor) extractGoTypeSpec(node *sitter.Node) *Definition {
	nameNode := de.parser.GetChildByType(node, "type_identifier")
	if nameNode == nil {
		return nil
	}

	name := de.parser.GetNodeText(nameNode, de.content)
	
	// Determine type kind
	var defType DefinitionType = DefType
	var summary string
	
	if structType := de.parser.GetChildByType(node, "struct_type"); structType != nil {
		defType = DefStruct
		summary = fmt.Sprintf("Struct %s", name)
	} else if interfaceType := de.parser.GetChildByType(node, "interface_type"); interfaceType != nil {
		defType = DefInterface
		summary = fmt.Sprintf("Interface %s", name)
	} else {
		summary = fmt.Sprintf("Type %s", name)
	}

	startLine, startCol, endLine, endCol := de.parser.GetNodeLocation(node)
	signature := de.parser.GetNodeText(node, de.content)

	// Look for documentation comment
	docString := de.extractGoDocComment(node)

	return &Definition{
		Type:        defType,
		Name:        name,
		Signature:   signature,
		Summary:     summary,
		StartLine:   startLine,
		StartColumn: startCol,
		EndLine:     endLine,
		EndColumn:   endCol,
		Content:     signature,
		DocString:   docString,
	}
}

func (de *DefinitionExtractor) extractGoVariables(node *sitter.Node) []Definition {
	var definitions []Definition

	de.parser.TraverseTree(node, func(child *sitter.Node, depth int) bool {
		if child.Type() == "var_spec" {
			defs := de.extractGoVarSpec(child)
			definitions = append(definitions, defs...)
		}
		return true
	})

	return definitions
}

func (de *DefinitionExtractor) extractGoVarSpec(node *sitter.Node) []Definition {
	var definitions []Definition

	// Get identifier list
	identifiers := de.parser.GetChildrenByType(node, "identifier")
	
	for _, idNode := range identifiers {
		name := de.parser.GetNodeText(idNode, de.content)
		startLine, startCol, endLine, endCol := de.parser.GetNodeLocation(idNode)

		def := Definition{
			Type:        DefVariable,
			Name:        name,
			Signature:   de.parser.GetNodeText(node, de.content),
			Summary:     fmt.Sprintf("Variable %s", name),
			StartLine:   startLine,
			StartColumn: startCol,
			EndLine:     endLine,
			EndColumn:   endCol,
			Content:     de.parser.GetNodeText(node, de.content),
		}

		definitions = append(definitions, def)
	}

	return definitions
}

func (de *DefinitionExtractor) extractGoConstants(node *sitter.Node) []Definition {
	var definitions []Definition

	de.parser.TraverseTree(node, func(child *sitter.Node, depth int) bool {
		if child.Type() == "const_spec" {
			defs := de.extractGoConstSpec(child)
			definitions = append(definitions, defs...)
		}
		return true
	})

	return definitions
}

func (de *DefinitionExtractor) extractGoConstSpec(node *sitter.Node) []Definition {
	var definitions []Definition

	// Get identifier list
	identifiers := de.parser.GetChildrenByType(node, "identifier")
	
	for _, idNode := range identifiers {
		name := de.parser.GetNodeText(idNode, de.content)
		startLine, startCol, endLine, endCol := de.parser.GetNodeLocation(idNode)

		def := Definition{
			Type:        DefConstant,
			Name:        name,
			Signature:   de.parser.GetNodeText(node, de.content),
			Summary:     fmt.Sprintf("Constant %s", name),
			StartLine:   startLine,
			StartColumn: startCol,
			EndLine:     endLine,
			EndColumn:   endCol,
			Content:     de.parser.GetNodeText(node, de.content),
		}

		definitions = append(definitions, def)
	}

	return definitions
}

// Helper methods for Go-specific extraction

func (de *DefinitionExtractor) extractGoFunctionSignature(node *sitter.Node) string {
	// Get the function signature up to the opening brace
	content := de.parser.GetNodeText(node, de.content)
	lines := strings.Split(content, "\n")
	
	var signatureLines []string
	for _, line := range lines {
		signatureLines = append(signatureLines, line)
		if strings.Contains(line, "{") {
			// Remove the opening brace and everything after
			if idx := strings.Index(line, "{"); idx >= 0 {
				signatureLines[len(signatureLines)-1] = strings.TrimSpace(line[:idx])
			}
			break
		}
	}
	
	return strings.Join(signatureLines, " ")
}

func (de *DefinitionExtractor) extractGoMethodSignature(node *sitter.Node) string {
	return de.extractGoFunctionSignature(node)
}

func (de *DefinitionExtractor) extractGoReceiver(node *sitter.Node) string {
	receiverNode := de.parser.GetChildByType(node, "parameter_list")
	if receiverNode == nil {
		return ""
	}
	
	return de.parser.GetNodeText(receiverNode, de.content)
}

func (de *DefinitionExtractor) extractGoFunctionParameters(node *sitter.Node) []Parameter {
	var params []Parameter
	
	// Find parameter list (skip receiver for methods)
	paramLists := de.parser.GetChildrenByType(node, "parameter_list")
	
	var paramList *sitter.Node
	if len(paramLists) >= 2 {
		// Method with receiver - use second parameter list
		paramList = paramLists[1]
	} else if len(paramLists) == 1 {
		// Function - use first parameter list
		paramList = paramLists[0]
	}
	
	if paramList == nil {
		return params
	}
	
	// Extract parameter declarations
	paramDecls := de.parser.GetChildrenByType(paramList, "parameter_declaration")
	for _, paramDecl := range paramDecls {
		identifiers := de.parser.GetChildrenByType(paramDecl, "identifier")
		typeNode := de.parser.GetChildByType(paramDecl, "type_identifier")
		if typeNode == nil {
			// Try other type nodes
			for _, child := range de.parser.GetChildrenByType(paramDecl, "*") {
				if strings.Contains(child.Type(), "type") {
					typeNode = child
					break
				}
			}
		}
		
		paramType := ""
		if typeNode != nil {
			paramType = de.parser.GetNodeText(typeNode, de.content)
		}
		
		for _, idNode := range identifiers {
			param := Parameter{
				Name: de.parser.GetNodeText(idNode, de.content),
				Type: paramType,
			}
			params = append(params, param)
		}
	}
	
	return params
}

func (de *DefinitionExtractor) extractGoReturnType(node *sitter.Node) string {
	// Look for result in parameter list after parameters
	paramLists := de.parser.GetChildrenByType(node, "parameter_list")
	
	var resultList *sitter.Node
	if len(paramLists) >= 3 {
		// Method with receiver and return type
		resultList = paramLists[2]
	} else if len(paramLists) == 2 {
		// Function with return type
		resultList = paramLists[1]
	}
	
	if resultList == nil {
		return ""
	}
	
	return de.parser.GetNodeText(resultList, de.content)
}

func (de *DefinitionExtractor) extractGoDocComment(node *sitter.Node) string {
	// This is a simplified implementation
	// In practice, you'd look for comment nodes preceding the declaration
	return ""
}

// Placeholder implementations for other languages

func (de *DefinitionExtractor) extractPythonDefinitions(root *sitter.Node) []Definition {
	var definitions []Definition
	
	de.parser.TraverseTree(root, func(node *sitter.Node, depth int) bool {
		switch node.Type() {
		case "function_definition":
			if def := de.extractPythonFunction(node); def != nil {
				definitions = append(definitions, *def)
			}
		case "class_definition":
			if def := de.extractPythonClass(node); def != nil {
				definitions = append(definitions, *def)
			}
		}
		return true
	})
	
	return definitions
}

func (de *DefinitionExtractor) extractPythonFunction(node *sitter.Node) *Definition {
	nameNode := de.parser.GetChildByType(node, "identifier")
	if nameNode == nil {
		return nil
	}

	name := de.parser.GetNodeText(nameNode, de.content)
	startLine, startCol, endLine, endCol := de.parser.GetNodeLocation(node)

	return &Definition{
		Type:        DefFunction,
		Name:        name,
		Signature:   de.parser.GetNodeText(node, de.content),
		Summary:     fmt.Sprintf("Python function %s", name),
		StartLine:   startLine,
		StartColumn: startCol,
		EndLine:     endLine,
		EndColumn:   endCol,
		Content:     de.parser.GetNodeText(node, de.content),
	}
}

func (de *DefinitionExtractor) extractPythonClass(node *sitter.Node) *Definition {
	nameNode := de.parser.GetChildByType(node, "identifier")
	if nameNode == nil {
		return nil
	}

	name := de.parser.GetNodeText(nameNode, de.content)
	startLine, startCol, endLine, endCol := de.parser.GetNodeLocation(node)

	return &Definition{
		Type:        DefClass,
		Name:        name,
		Signature:   de.parser.GetNodeText(node, de.content),
		Summary:     fmt.Sprintf("Python class %s", name),
		StartLine:   startLine,
		StartColumn: startCol,
		EndLine:     endLine,
		EndColumn:   endCol,
		Content:     de.parser.GetNodeText(node, de.content),
	}
}

func (de *DefinitionExtractor) extractJavaScriptDefinitions(root *sitter.Node) []Definition {
	var definitions []Definition
	
	de.parser.TraverseTree(root, func(node *sitter.Node, depth int) bool {
		switch node.Type() {
		case "function_declaration":
			if def := de.extractJavaScriptFunction(node); def != nil {
				definitions = append(definitions, *def)
			}
		case "class_declaration":
			if def := de.extractJavaScriptClass(node); def != nil {
				definitions = append(definitions, *def)
			}
		}
		return true
	})
	
	return definitions
}

func (de *DefinitionExtractor) extractJavaScriptFunction(node *sitter.Node) *Definition {
	nameNode := de.parser.GetChildByType(node, "identifier")
	if nameNode == nil {
		return nil
	}

	name := de.parser.GetNodeText(nameNode, de.content)
	startLine, startCol, endLine, endCol := de.parser.GetNodeLocation(node)

	return &Definition{
		Type:        DefFunction,
		Name:        name,
		Signature:   de.parser.GetNodeText(node, de.content),
		Summary:     fmt.Sprintf("JavaScript function %s", name),
		StartLine:   startLine,
		StartColumn: startCol,
		EndLine:     endLine,
		EndColumn:   endCol,
		Content:     de.parser.GetNodeText(node, de.content),
	}
}

func (de *DefinitionExtractor) extractJavaScriptClass(node *sitter.Node) *Definition {
	nameNode := de.parser.GetChildByType(node, "identifier")
	if nameNode == nil {
		return nil
	}

	name := de.parser.GetNodeText(nameNode, de.content)
	startLine, startCol, endLine, endCol := de.parser.GetNodeLocation(node)

	return &Definition{
		Type:        DefClass,
		Name:        name,
		Signature:   de.parser.GetNodeText(node, de.content),
		Summary:     fmt.Sprintf("JavaScript class %s", name),
		StartLine:   startLine,
		StartColumn: startCol,
		EndLine:     endLine,
		EndColumn:   endCol,
		Content:     de.parser.GetNodeText(node, de.content),
	}
}

func (de *DefinitionExtractor) extractRustDefinitions(root *sitter.Node) []Definition {
	var definitions []Definition
	
	de.parser.TraverseTree(root, func(node *sitter.Node, depth int) bool {
		switch node.Type() {
		case "function_item":
			if def := de.extractRustFunction(node); def != nil {
				definitions = append(definitions, *def)
			}
		case "struct_item":
			if def := de.extractRustStruct(node); def != nil {
				definitions = append(definitions, *def)
			}
		}
		return true
	})
	
	return definitions
}

func (de *DefinitionExtractor) extractRustFunction(node *sitter.Node) *Definition {
	nameNode := de.parser.GetChildByType(node, "identifier")
	if nameNode == nil {
		return nil
	}

	name := de.parser.GetNodeText(nameNode, de.content)
	startLine, startCol, endLine, endCol := de.parser.GetNodeLocation(node)

	return &Definition{
		Type:        DefFunction,
		Name:        name,
		Signature:   de.parser.GetNodeText(node, de.content),
		Summary:     fmt.Sprintf("Rust function %s", name),
		StartLine:   startLine,
		StartColumn: startCol,
		EndLine:     endLine,
		EndColumn:   endCol,
		Content:     de.parser.GetNodeText(node, de.content),
	}
}

func (de *DefinitionExtractor) extractRustStruct(node *sitter.Node) *Definition {
	nameNode := de.parser.GetChildByType(node, "type_identifier")
	if nameNode == nil {
		return nil
	}

	name := de.parser.GetNodeText(nameNode, de.content)
	startLine, startCol, endLine, endCol := de.parser.GetNodeLocation(node)

	return &Definition{
		Type:        DefStruct,
		Name:        name,
		Signature:   de.parser.GetNodeText(node, de.content),
		Summary:     fmt.Sprintf("Rust struct %s", name),
		StartLine:   startLine,
		StartColumn: startCol,
		EndLine:     endLine,
		EndColumn:   endCol,
		Content:     de.parser.GetNodeText(node, de.content),
	}
}