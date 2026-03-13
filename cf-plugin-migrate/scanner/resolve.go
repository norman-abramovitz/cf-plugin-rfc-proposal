package scanner

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"strings"
)

// debugResolve enables debug output for endpoint resolution tracing.
var debugResolve = os.Getenv("CF_MIGRATE_DEBUG") != ""

func debugf(format string, args ...interface{}) {
	if debugResolve {
		fmt.Fprintf(os.Stderr, "[resolve] "+format, args...)
	}
}

// parsedFile holds a parsed Go source file for cross-function analysis.
type parsedFile struct {
	path string
	fset *token.FileSet
	file *ast.File
}

// ResolvedEndpoint represents a URL endpoint found by tracing through wrapper functions.
type ResolvedEndpoint struct {
	Endpoint string // the resolved URL string literal
	File     string
	Line     int
	Caller   string // function name at the call site

	V3Endpoint string // suggested V3 equivalent
	V3Notes    string
}

// wrapperFunc tracks a function that passes a URL parameter through to CliCommand.
type wrapperFunc struct {
	name       string // function name (unqualified)
	paramIndex int    // which parameter (0-based) carries the URL
}

// resolveCurlEndpoints traces dynamic URL parameters backwards through the call chain
// to find the string literals that ultimately get passed to CliCommand("curl", url).
//
// Starting from the function containing CliCommand("curl", paramName), it identifies
// wrapper functions that forward the URL parameter, then finds their callers, recursing
// until it reaches string literals.
func resolveCurlEndpoints(files []*parsedFile, result *ScanResult) {
	if len(result.CliCommandCalls) == 0 {
		return
	}

	// For each curl call with a dynamic endpoint, trace backwards.
	for _, cc := range result.CliCommandCalls {
		if cc.Command != "curl" || cc.EndpointVar == "" {
			continue
		}

		// Find the function containing this curl call.
		containingFunc, _ := findContainingFunc(files, cc.File, cc.Line)
		if containingFunc == nil {
			continue
		}

		// Check if the endpoint variable is a function parameter.
		paramIdx := findParamIndex(containingFunc, cc.EndpointVar)
		if paramIdx < 0 {
			// EndpointVar is a local variable, not a parameter — can't trace further.
			continue
		}

		// Seed the worklist with the function containing CliCommand.
		worklist := []wrapperFunc{{
			name:       containingFunc.Name.Name,
			paramIndex: paramIdx,
		}}
		seen := map[string]bool{containingFunc.Name.Name: true}

		// Also check if there are intermediate wrapper functions in the same file
		// that call the containing function and forward a parameter.
		// We trace transitively until we find string literals.
		var resolved []ResolvedEndpoint

		for len(worklist) > 0 {
			current := worklist[0]
			worklist = worklist[1:]

			debugf("tracing callers of %s (paramIdx=%d)\n", current.name, current.paramIndex)

			// Scan all functions for calls to current.name.
			nFuncs := 0
			for _, pf := range files {
				for _, decl := range pf.file.Decls {
					fn, ok := decl.(*ast.FuncDecl)
					if !ok || fn.Body == nil {
						continue
					}
					nFuncs++
					findCallsTo(pf, fn, current, &resolved, &worklist, seen)
				}
			}
			debugf("  scanned %d functions across %d files\n", nFuncs, len(files))
		}

		cc.ResolvedEndpoints = resolved

		// Also look up V3 equivalents for each resolved endpoint.
		for i := range cc.ResolvedEndpoints {
			ep := &cc.ResolvedEndpoints[i]
			if v3, ok := lookupV3Endpoint(ep.Endpoint); ok {
				ep.V3Endpoint = v3.V3Path
				ep.V3Notes = v3.Notes
			}
		}
	}
}

// findCallsTo scans a function body for calls to target.name and extracts
// the argument at target.paramIndex. String literals are recorded as resolved
// endpoints. Parameters that forward through are added to the worklist.
func findCallsTo(
	pf *parsedFile,
	fn *ast.FuncDecl,
	target wrapperFunc,
	resolved *[]ResolvedEndpoint,
	worklist *[]wrapperFunc,
	seen map[string]bool,
) {
	// Track local string variable assignments for resolution.
	stringVars := make(map[string]stringLitInfo)

	// Track variables that alias function parameters: nextUrl := url (where url is a param)
	paramAliases := make(map[string]int) // varName → param index it aliases
	// Seed with actual parameter names.
	if fn.Type.Params != nil {
		idx := 0
		for _, field := range fn.Type.Params.List {
			for _, name := range field.Names {
				paramAliases[name.Name] = idx
				idx++
			}
			if len(field.Names) == 0 {
				idx++
			}
		}
	}

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		// Track string literal assignments: url := "/v2/apps"
		// Also track assignments from parameters: nextUrl := url
		if assign, ok := n.(*ast.AssignStmt); ok {
			if len(assign.Lhs) == 1 && len(assign.Rhs) == 1 {
				if ident, ok := assign.Lhs[0].(*ast.Ident); ok {
					if lit, ok := assign.Rhs[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						stringVars[ident.Name] = stringLitInfo{
							value: strings.Trim(lit.Value, `"`),
							pos:   lit.Pos(),
						}
					}
					// Track assignment from a parameter alias: nextUrl := url
					if rhsIdent, ok := assign.Rhs[0].(*ast.Ident); ok {
						if paramIdx, isParam := paramAliases[rhsIdent.Name]; isParam {
							paramAliases[ident.Name] = paramIdx
						}
					}
					// Also handle fmt.Sprintf("/v2/routes/%v/apps", routeId)
					if val, pos, ok := extractFmtSprintf(assign.Rhs[0]); ok {
						stringVars[ident.Name] = stringLitInfo{value: val, pos: pos}
					}
				}
			}
		}

		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		// Match calls to the target function by name.
		callName := extractCallName(call)
		if callName == "CallPagableAPI" && target.name == "CallPagableAPI" {
			pos := pf.fset.Position(call.Pos())
			debugf("  ** CallPagableAPI call at %s:%d in func %s\n", pf.path, pos.Line, fn.Name.Name)
		}
		if callName != target.name {
			return true
		}
		debugf("  found call to %s in %s.%s (nargs=%d, targetIdx=%d)\n", target.name, pf.path, fn.Name.Name, len(call.Args), target.paramIndex)

		// Check the argument at the target parameter index.
		if target.paramIndex >= len(call.Args) {
			return true
		}
		arg := call.Args[target.paramIndex]

		switch a := arg.(type) {
		case *ast.BasicLit:
			if a.Kind == token.STRING {
				pos := pf.fset.Position(a.Pos())
				*resolved = append(*resolved, ResolvedEndpoint{
					Endpoint: strings.Trim(a.Value, `"`),
					File:     pf.path,
					Line:     pos.Line,
					Caller:   fn.Name.Name,
				})
			}
		case *ast.Ident:
			// Check if it's a tracked string variable.
			if si, ok := stringVars[a.Name]; ok {
				pos := pf.fset.Position(call.Pos())
				debugf("  resolved: %s in %s:%d passes literal %q\n", fn.Name.Name, pf.path, pos.Line, si.value)
				*resolved = append(*resolved, ResolvedEndpoint{
					Endpoint: si.value,
					File:     pf.path,
					Line:     pos.Line,
					Caller:   fn.Name.Name,
				})
			} else if paramIdx, isParam := paramAliases[a.Name]; isParam {
				// Variable is a function parameter or alias of one — add to worklist.
				debugf("  wrapper: %s forwards param %s (idx=%d) to %s\n", fn.Name.Name, a.Name, paramIdx, target.name)
				if !seen[fn.Name.Name] {
					seen[fn.Name.Name] = true
					*worklist = append(*worklist, wrapperFunc{
						name:       fn.Name.Name,
						paramIndex: paramIdx,
					})
				}
			} else {
				debugf("  unresolved: %s in %s passes var %s to %s (not tracked)\n", fn.Name.Name, pf.path, a.Name, target.name)
			}
		}
		return true
	})
}

type stringLitInfo struct {
	value string
	pos   token.Pos
}

// extractCallName returns the function name from a call expression.
// Handles both plain calls (callCurl(...)) and selector calls (common.CallAPI(...)).
func extractCallName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		return fn.Sel.Name
	}
	return ""
}

// findParamIndex returns the 0-based parameter index of a named parameter,
// or -1 if the name is not a parameter.
func findParamIndex(fn *ast.FuncDecl, name string) int {
	if fn.Type.Params == nil {
		return -1
	}
	idx := 0
	for _, field := range fn.Type.Params.List {
		for _, ident := range field.Names {
			if ident.Name == name {
				return idx
			}
			idx++
		}
		// Handle unnamed parameters (shouldn't happen in practice).
		if len(field.Names) == 0 {
			idx++
		}
	}
	return -1
}

// findContainingFunc finds the function declaration containing the given file:line.
func findContainingFunc(files []*parsedFile, filePath string, line int) (*ast.FuncDecl, *parsedFile) {
	for _, pf := range files {
		if pf.path != filePath {
			continue
		}
		for _, decl := range pf.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			start := pf.fset.Position(fn.Pos()).Line
			end := pf.fset.Position(fn.End()).Line
			if line >= start && line <= end {
				return fn, pf
			}
		}
	}
	return nil, nil
}

// methodKey returns a key for method declarations: "TypeName.MethodName".
func methodKey(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	recv := fn.Recv.List[0].Type
	// Dereference pointer receiver.
	if star, ok := recv.(*ast.StarExpr); ok {
		recv = star.X
	}
	if ident, ok := recv.(*ast.Ident); ok {
		return ident.Name + "." + fn.Name.Name
	}
	return fn.Name.Name
}

// extractFmtSprintf extracts the format string from fmt.Sprintf calls.
// Returns the format string, position, and true if successful.
func extractFmtSprintf(expr ast.Expr) (string, token.Pos, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return "", 0, false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", 0, false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "fmt" || sel.Sel.Name != "Sprintf" {
		return "", 0, false
	}
	if len(call.Args) < 1 {
		return "", 0, false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", 0, false
	}
	return strings.Trim(lit.Value, `"`), lit.Pos(), true
}
