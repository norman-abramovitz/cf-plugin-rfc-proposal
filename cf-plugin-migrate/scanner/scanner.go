package scanner

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// ScanResult holds the aggregated scan output.
type ScanResult struct {
	Package             string
	Methods             map[string]*MethodResult
	CliCommandCalls     []*CliCommandCall
	InternalImports     []*InternalImport
	DiscoveredEndpoints []*DiscoveredEndpoint
}

// InternalImport records a CLI internal package import detected in guest code.
type InternalImport struct {
	File        string
	ImportPath  string
	Replacement string // cf-plugin-helpers replacement path, or empty
	Note        string // guidance for the developer
}

// MethodResult holds detected fields for one V2 method.
type MethodResult struct {
	Fields    map[string]bool            // top-level fields accessed
	SubFields map[string]map[string]bool // subFieldKey → set of sub-field paths
	CallSites []CallSite
}

// CallSite records where a V2 method is called.
type CallSite struct {
	File     string
	Line     int
	VarName  string // the variable assigned from the call
	Flagged  bool   // true if result is used in a way the scanner can't fully trace
	FlagNote string
}

// Scan parses Go source files matching the given patterns and finds
// V2 domain method calls and field access.
func Scan(patterns []string) (*ScanResult, error) {
	result := &ScanResult{
		Methods: make(map[string]*MethodResult),
	}

	var parsed []*parsedFile

	for _, pattern := range patterns {
		files, err := resolvePattern(pattern)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			pf, err := scanFile(file, result)
			if err != nil {
				return nil, fmt.Errorf("scanning %s: %w", file, err)
			}
			if pf != nil {
				parsed = append(parsed, pf)
			}
		}
	}

	// Second pass: resolve dynamic curl endpoints through wrapper functions.
	resolveCurlEndpoints(parsed, result)

	// Third pass: discover all API endpoint string literals in the source.
	discoverEndpoints(parsed, result)

	return result, nil
}

func resolvePattern(pattern string) ([]string, error) {
	if strings.HasSuffix(pattern, "/...") {
		dir := strings.TrimSuffix(pattern, "/...")
		if dir == "." || dir == "" {
			dir = "."
		}
		return findGoFiles(dir)
	}
	return findGoFilesInDir(pattern)
}

func findGoFiles(root string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && info.Name() == "vendor" {
			return filepath.SkipDir
		}
		if !info.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func findGoFilesInDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	return files, nil
}

func scanFile(path string, result *ScanResult) (*parsedFile, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}

	pf := &parsedFile{path: path, fset: fset, file: f}

	if result.Package == "" {
		result.Package = f.Name.Name
	}

	// Detect CLI internal package imports.
	for _, imp := range f.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)
		if !isCLIImport(importPath) {
			continue
		}
		if isAllowedImport(importPath) {
			continue
		}

		ii := &InternalImport{
			File:       path,
			ImportPath: importPath,
		}
		if repl, ok := InternalImportReplacements[importPath]; ok {
			ii.Replacement = repl.Replacement
			ii.Note = repl.Note
		} else {
			ii.Note = "Unknown CLI internal package — review manually"
		}
		result.InternalImports = append(result.InternalImports, ii)
	}

	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		scanFunction(fset, path, fn, result)
		scanFunctionForCliCommands(fset, path, fn, result)
	}

	return pf, nil
}

// rangeInfo tracks a range variable's origin.
type rangeInfo struct {
	parentVar   string
	parentField string
}

func scanFunction(fset *token.FileSet, path string, fn *ast.FuncDecl, result *ScanResult) {
	// Phase 1: Find V2 method call sites and their result variables.
	resultVars := make(map[string]string) // varName → V2 method name
	rangeVars := make(map[string]rangeInfo)

	var callSites []struct {
		method  string
		varName string
		pos     token.Pos
		flagged bool
		note    string
	}

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch stmt := n.(type) {
		case *ast.AssignStmt:
			if len(stmt.Rhs) != 1 {
				return true
			}
			method := extractV2Call(stmt.Rhs[0])
			if method == "" {
				return true
			}
			if len(stmt.Lhs) >= 1 {
				if ident, ok := stmt.Lhs[0].(*ast.Ident); ok {
					resultVars[ident.Name] = method
					callSites = append(callSites, struct {
						method  string
						varName string
						pos     token.Pos
						flagged bool
						note    string
					}{method, ident.Name, stmt.Pos(), false, ""})
				}
			}

		case *ast.ReturnStmt:
			// Track V2 calls in return statements — result flows to caller.
			for _, expr := range stmt.Results {
				method := extractV2Call(expr)
				if method == "" {
					continue
				}
				callSites = append(callSites, struct {
					method  string
					varName string
					pos     token.Pos
					flagged bool
					note    string
				}{method, "", stmt.Pos(), true,
					"result returned to caller — field access may be in calling function"})
			}

		case *ast.RangeStmt:
			// Track: for _, x := range resultVar.Field
			if sel, ok := stmt.X.(*ast.SelectorExpr); ok {
				ident, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}
				if _, tracked := resultVars[ident.Name]; !tracked {
					return true
				}
				if stmt.Value != nil {
					if valIdent, ok := stmt.Value.(*ast.Ident); ok {
						rangeVars[valIdent.Name] = rangeInfo{
							parentVar:   ident.Name,
							parentField: sel.Sel.Name,
						}
					}
				}
			}
			// Track: for _, x := range resultVar (plain slice)
			// e.g., apps, _ := conn.GetApps(); for _, app := range apps
			if ident, ok := stmt.X.(*ast.Ident); ok {
				if _, tracked := resultVars[ident.Name]; tracked {
					if stmt.Value != nil {
						if valIdent, ok := stmt.Value.(*ast.Ident); ok {
							rangeVars[valIdent.Name] = rangeInfo{
								parentVar:   ident.Name,
								parentField: "", // direct element, no parent field
							}
						}
					}
				}
			}
		}
		return true
	})

	if len(callSites) == 0 {
		return
	}

	// Phase 2: Find field accesses on result variables and range variables.
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		chain := resolveSelectorChain(sel)
		if len(chain) < 2 {
			return true
		}

		rootVar := chain[0]

		// Direct access on a result variable: app.Guid, app.Routes[i].Host
		if method, ok := resultVars[rootVar]; ok {
			fieldPath := chain[1:]
			recordFieldAccess(result, method, fieldPath)
			return false // don't recurse into inner selectors
		}

		// Access on a range variable: route.Host, route.Domain.Name
		if ri, ok := rangeVars[rootVar]; ok {
			if method, ok := resultVars[ri.parentVar]; ok {
				if ri.parentField == "" {
					// Plain slice range: field access is directly on the element
					recordFieldAccess(result, method, chain[1:])
				} else {
					fieldPath := make([]string, 0, 1+len(chain)-1)
					fieldPath = append(fieldPath, ri.parentField)
					fieldPath = append(fieldPath, chain[1:]...)
					recordFieldAccess(result, method, fieldPath)
				}
				return false
			}
		}

		return true
	})

	// Record call sites.
	for _, cs := range callSites {
		pos := fset.Position(cs.pos)
		mr := getOrCreateMethod(result, cs.method)
		mr.CallSites = append(mr.CallSites, CallSite{
			File:     path,
			Line:     pos.Line,
			VarName:  cs.varName,
			Flagged:  cs.flagged,
			FlagNote: cs.note,
		})
	}
}

// extractV2Call checks if an expression is a call to a V2 method.
// It handles both simple calls (conn.GetApp()) and deeper chains
// (services.CLI.GetApps()). Returns the method name or "".
func extractV2Call(expr ast.Expr) string {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return ""
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	if V2Methods[sel.Sel.Name] {
		return sel.Sel.Name
	}
	return ""
}

// resolveSelectorChain walks a SelectorExpr tree and returns the chain
// of identifiers. E.g., app.Routes[i].Domain.Name → ["app", "Routes", "Domain", "Name"]
func resolveSelectorChain(expr ast.Expr) []string {
	switch e := expr.(type) {
	case *ast.SelectorExpr:
		chain := resolveSelectorChain(e.X)
		if chain == nil {
			return nil
		}
		return append(chain, e.Sel.Name)
	case *ast.Ident:
		return []string{e.Name}
	case *ast.IndexExpr:
		return resolveSelectorChain(e.X)
	default:
		return nil
	}
}

// recordFieldAccess categorizes a field path and records it in the result.
func recordFieldAccess(result *ScanResult, method string, fieldPath []string) {
	if len(fieldPath) == 0 {
		return
	}

	mr := getOrCreateMethod(result, method)
	modelInfo := V2Models[method]
	if modelInfo == nil {
		return
	}

	topField := fieldPath[0]

	// Only record known fields.
	if _, known := modelInfo.FieldGroup[topField]; !known {
		return
	}

	mr.Fields[topField] = true

	// If accessing sub-fields of a composite type, record them.
	// Only record sub-fields that start with uppercase — V2 model fields are
	// always exported. Lowercase names indicate scope leaks from variable
	// shadowing (e.g., range var reusing a receiver name).
	if len(fieldPath) > 1 && isExported(fieldPath[1]) {
		if subFieldKey, ok := modelInfo.SubFieldKeys[topField]; ok {
			subField := strings.Join(fieldPath[1:], ".")
			if mr.SubFields[subFieldKey] == nil {
				mr.SubFields[subFieldKey] = make(map[string]bool)
			}
			mr.SubFields[subFieldKey][subField] = true
		}
	}
}

// isExported reports whether a field name starts with an uppercase letter.
func isExported(name string) bool {
	for _, r := range name {
		return unicode.IsUpper(r)
	}
	return false
}

func getOrCreateMethod(result *ScanResult, method string) *MethodResult {
	mr, ok := result.Methods[method]
	if !ok {
		mr = &MethodResult{
			Fields:    make(map[string]bool),
			SubFields: make(map[string]map[string]bool),
		}
		result.Methods[method] = mr
	}
	return mr
}
