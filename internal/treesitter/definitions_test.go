package treesitter

import (
	"testing"
)

func extractDefs(t *testing.T, lang Language, src string) []Definition {
	t.Helper()
	parser := newTestParser(t)
	res, err := parser.ParseContent(src, lang, "test-src")
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	extractor := NewDefinitionExtractor(parser)
	defs, err := extractor.ExtractDefinitions(res)
	if err != nil {
		t.Fatalf("ExtractDefinitions: %v", err)
	}
	return defs
}

func findDef(defs []Definition, name string, typ DefinitionType) *Definition {
	for i := range defs {
		if defs[i].Name == name && defs[i].Type == typ {
			return &defs[i]
		}
	}
	return nil
}

const goSource = `package calc

import (
	"fmt"
	str "strings"
)

const Pi = 3.14

var GlobalCount int

type Shape interface {
	Area() float64
}

type Rectangle struct {
	Width  float64
	Height float64
}

func Add(a int, b int) int {
	return a + b
}

func (r Rectangle) Area() float64 {
	return r.Width * r.Height
}
`

func TestExtractGoDefinitions(t *testing.T) {
	defs := extractDefs(t, LanguageGo, goSource)

	// Package.
	pkg := findDef(defs, "calc", DefPackage)
	if pkg == nil {
		t.Fatal("missing package definition 'calc'")
	}
	if pkg.Signature != "package calc" {
		t.Errorf("package signature = %q", pkg.Signature)
	}
	if pkg.StartLine != 1 {
		t.Errorf("package StartLine = %d, want 1", pkg.StartLine)
	}

	// Imports - "fmt" plain and "strings" aliased as str.
	fmtImp := findDef(defs, "fmt", DefImport)
	if fmtImp == nil {
		t.Fatal("missing import 'fmt'")
	}
	if fmtImp.Metadata["import_path"] != "fmt" {
		t.Errorf("fmt import_path = %v", fmtImp.Metadata["import_path"])
	}
	strImp := findDef(defs, "str", DefImport)
	if strImp == nil {
		t.Fatal("missing aliased import 'str'")
	}
	if strImp.Metadata["import_path"] != "strings" {
		t.Errorf("str import_path = %v, want strings", strImp.Metadata["import_path"])
	}
	if strImp.Metadata["alias"] != "str" {
		t.Errorf("str alias = %v, want str", strImp.Metadata["alias"])
	}

	// Constant.
	if c := findDef(defs, "Pi", DefConstant); c == nil {
		t.Error("missing constant 'Pi'")
	}

	// Variable.
	if v := findDef(defs, "GlobalCount", DefVariable); v == nil {
		t.Error("missing variable 'GlobalCount'")
	}

	// Interface.
	iface := findDef(defs, "Shape", DefInterface)
	if iface == nil {
		t.Fatal("missing interface 'Shape'")
	}
	if iface.Summary != "Interface Shape" {
		t.Errorf("interface summary = %q", iface.Summary)
	}

	// Struct.
	rect := findDef(defs, "Rectangle", DefStruct)
	if rect == nil {
		t.Fatal("missing struct 'Rectangle'")
	}
	if rect.Summary != "Struct Rectangle" {
		t.Errorf("struct summary = %q", rect.Summary)
	}

	// Function with parameters.
	add := findDef(defs, "Add", DefFunction)
	if add == nil {
		t.Fatal("missing function 'Add'")
	}
	if len(add.Parameters) != 2 {
		t.Fatalf("Add params = %d, want 2", len(add.Parameters))
	}
	if add.Parameters[0].Name != "a" || add.Parameters[0].Type != "int" {
		t.Errorf("Add param0 = %+v, want {a int}", add.Parameters[0])
	}
	if add.Parameters[1].Name != "b" {
		t.Errorf("Add param1 name = %q, want b", add.Parameters[1].Name)
	}
	if add.Signature != "func Add(a int, b int) int" {
		t.Errorf("Add signature = %q", add.Signature)
	}
	// Add is declared on line 21 of goSource (1-indexed: "package calc" is
	// line 1; the func sits after the import block, the const/var, and the
	// Shape interface + Rectangle struct declarations).
	if add.StartLine != 21 {
		t.Errorf("Add StartLine = %d, want 21", add.StartLine)
	}
	if add.FilePath != "test-src" {
		t.Errorf("Add FilePath = %q", add.FilePath)
	}
	if add.Language != LanguageGo {
		t.Errorf("Add Language = %q", add.Language)
	}

	// Method with receiver.
	area := findDef(defs, "Area", DefMethod)
	if area == nil {
		t.Fatal("missing method 'Area'")
	}
	if area.Receiver == "" {
		t.Error("method Area should have a receiver")
	}
	if area.Summary != "Method Area" {
		t.Errorf("Area summary = %q", area.Summary)
	}
}

func TestExtractDefinitionsByType(t *testing.T) {
	parser := newTestParser(t)
	res, err := parser.ParseContent(goSource, LanguageGo, "test-src")
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	extractor := NewDefinitionExtractor(parser)

	funcs, err := extractor.ExtractDefinitionsByType(res, DefFunction)
	if err != nil {
		t.Fatalf("ExtractDefinitionsByType: %v", err)
	}
	if len(funcs) != 1 {
		t.Errorf("function count = %d, want 1", len(funcs))
	}
	for _, f := range funcs {
		if f.Type != DefFunction {
			t.Errorf("got non-function in filtered result: %q", f.Type)
		}
	}

	imports, err := extractor.ExtractDefinitionsByType(res, DefImport)
	if err != nil {
		t.Fatalf("ExtractDefinitionsByType imports: %v", err)
	}
	if len(imports) != 2 {
		t.Errorf("import count = %d, want 2", len(imports))
	}
}

func TestFindDefinition(t *testing.T) {
	parser := newTestParser(t)
	res, err := parser.ParseContent(goSource, LanguageGo, "test-src")
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	extractor := NewDefinitionExtractor(parser)

	def, err := extractor.FindDefinition(res, "Rectangle")
	if err != nil {
		t.Fatalf("FindDefinition: %v", err)
	}
	if def.Type != DefStruct {
		t.Errorf("Rectangle type = %q, want struct", def.Type)
	}

	if _, err := extractor.FindDefinition(res, "DoesNotExist"); err == nil {
		t.Error("expected error for missing definition")
	}
}

func TestExtractDefinitionsInvalidResult(t *testing.T) {
	parser := newTestParser(t)
	extractor := NewDefinitionExtractor(parser)
	_, err := extractor.ExtractDefinitions(&ParseResult{Language: LanguageGo})
	if err == nil {
		t.Fatal("expected error for parse result with nil tree")
	}
}

func TestExtractDefinitionsUnsupportedLanguage(t *testing.T) {
	parser := newTestParser(t)
	// Build a result whose tree is valid but language is unsupported for extraction.
	res, err := parser.ParseContent("package main\n", LanguageGo, "x.go")
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	res.Language = LanguageJava
	extractor := NewDefinitionExtractor(parser)
	if _, err := extractor.ExtractDefinitions(res); err == nil {
		t.Fatal("expected error for unsupported extraction language")
	}
}

func TestExtractPythonDefinitions(t *testing.T) {
	src := "import os\n\nclass Animal:\n    def speak(self):\n        return 'noise'\n\ndef greet(name):\n    return 'hi ' + name\n"
	defs := extractDefs(t, LanguagePython, src)

	cls := findDef(defs, "Animal", DefClass)
	if cls == nil {
		t.Fatal("missing python class 'Animal'")
	}
	if cls.Summary != "Python class Animal" {
		t.Errorf("class summary = %q", cls.Summary)
	}
	if cls.StartLine != 3 {
		t.Errorf("class StartLine = %d, want 3", cls.StartLine)
	}

	// Both the method 'speak' and top-level 'greet' are function_definition nodes.
	greet := findDef(defs, "greet", DefFunction)
	if greet == nil {
		t.Fatal("missing python function 'greet'")
	}
	if greet.StartLine != 7 {
		t.Errorf("greet StartLine = %d, want 7", greet.StartLine)
	}
	if findDef(defs, "speak", DefFunction) == nil {
		t.Error("missing python method 'speak'")
	}
}

func TestExtractJavaScriptDefinitions(t *testing.T) {
	src := "function add(a, b) {\n  return a + b;\n}\n\nclass Widget {\n  render() {}\n}\n"
	defs := extractDefs(t, LanguageJavaScript, src)

	add := findDef(defs, "add", DefFunction)
	if add == nil {
		t.Fatal("missing JS function 'add'")
	}
	if add.Summary != "JavaScript function add" {
		t.Errorf("add summary = %q", add.Summary)
	}
	if add.StartLine != 1 {
		t.Errorf("add StartLine = %d, want 1", add.StartLine)
	}

	widget := findDef(defs, "Widget", DefClass)
	if widget == nil {
		t.Fatal("missing JS class 'Widget'")
	}
	if widget.Summary != "JavaScript class Widget" {
		t.Errorf("Widget summary = %q", widget.Summary)
	}
}

func TestExtractRustDefinitions(t *testing.T) {
	src := "struct Point {\n    x: i32,\n    y: i32,\n}\n\nfn distance() -> f64 {\n    0.0\n}\n"
	defs := extractDefs(t, LanguageRust, src)

	pt := findDef(defs, "Point", DefStruct)
	if pt == nil {
		t.Fatal("missing Rust struct 'Point'")
	}
	if pt.Summary != "Rust struct Point" {
		t.Errorf("Point summary = %q", pt.Summary)
	}

	dist := findDef(defs, "distance", DefFunction)
	if dist == nil {
		t.Fatal("missing Rust function 'distance'")
	}
	if dist.StartLine != 6 {
		t.Errorf("distance StartLine = %d, want 6", dist.StartLine)
	}
}

func TestExtractGoDefinitionsEmptyFile(t *testing.T) {
	defs := extractDefs(t, LanguageGo, "")
	if len(defs) != 0 {
		t.Errorf("empty file should yield 0 definitions, got %d", len(defs))
	}
}
