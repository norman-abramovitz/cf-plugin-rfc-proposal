package scanner

import (
	"go/ast"
	"go/token"
	"strings"
)

// CliCommandCall represents a detected CliCommand/CliCommandWithoutTerminalOutput call.
// When Command is "curl", the curl-specific fields (Endpoint, V3Endpoint, TargetVar, etc.)
// are populated via json.Unmarshal tracing.
type CliCommandCall struct {
	File    string
	Line    int
	Method  string   // "CliCommand" or "CliCommandWithoutTerminalOutput"
	Command string   // first argument: "curl", "apps", "push", etc.
	Args    []string // remaining literal arguments (best-effort extraction)

	// Curl-specific fields (populated only when Command == "curl")
	Endpoint    string          // URL literal value, or "" if unresolved
	EndpointVar string          // variable name if URL is from a variable
	ResultVar   string          // the []string result variable name
	TargetVar   string          // variable holding the unmarshalled result
	TargetType  string          // type name of the target variable
	Fields      map[string]bool // field paths accessed on the target/range variables
	V3Endpoint  string          // suggested V3 equivalent
	V3Notes     string          // migration notes

	// ResolvedEndpoints holds endpoints found by tracing the URL parameter
	// backwards through wrapper functions to the original string literals.
	ResolvedEndpoints []ResolvedEndpoint
}

// V2Endpoint holds the V3 equivalent for a V2 API path.
type V2Endpoint struct {
	V3Path string
	Notes  string
}

// V2EndpointMap maps known V2 API endpoint base paths (without leading slash) to V3 equivalents.
var V2EndpointMap = map[string]V2Endpoint{
	"v2/apps":                        {"/v3/apps", "V2 entity/metadata envelope → V3 flat resources"},
	"v2/spaces":                      {"/v3/spaces", ""},
	"v2/organizations":               {"/v3/organizations", ""},
	"v2/service_instances":           {"/v3/service_instances", ""},
	"v2/routes":                      {"/v3/routes", ""},
	"v2/domains":                     {"/v3/domains", "Private/shared domain distinction changed in V3"},
	"v2/users":                       {"/v3/users + /v3/roles", "Roles separated from users in V3"},
	"v2/service_plans":               {"/v3/service_plans", ""},
	"v2/services":                    {"/v3/service_offerings", "Renamed from services to service_offerings"},
	"v2/service_bindings":            {"/v3/service_credential_bindings", "Renamed in V3"},
	"v2/buildpacks":                  {"/v3/buildpacks", ""},
	"v2/stacks":                      {"/v3/stacks", ""},
	"v2/security_groups":             {"/v3/security_groups", ""},
	"v2/quota_definitions":           {"/v3/organization_quotas", "Renamed in V3"},
	"v2/space_quota_definitions":     {"/v3/space_quotas", "Renamed in V3"},
	"v2/events":                      {"/v3/audit_events", "Renamed in V3"},
	"v2/shared_domains":              {"/v3/domains", "Merged with private domains in V3"},
	"v2/private_domains":             {"/v3/domains", "Merged with shared domains in V3"},
	"v2/service_keys":                {"/v3/service_credential_bindings", "Type=key in V3"},
	"v2/environment_variable_groups": {"/v3/environment_variable_groups", ""},
}

// normalizeEndpoint strips leading slash and query parameters to get the base path.
func normalizeEndpoint(endpoint string) string {
	ep := strings.TrimPrefix(endpoint, "/")
	if idx := strings.IndexByte(ep, '?'); idx >= 0 {
		ep = ep[:idx]
	}
	// Keep only the first two path segments (e.g., "v2/apps" from "v2/apps/GUID/stats")
	parts := strings.SplitN(ep, "/", 3)
	if len(parts) >= 2 {
		return parts[0] + "/" + parts[1]
	}
	return ep
}

// lookupV3Endpoint finds the V3 equivalent for a V2 endpoint.
func lookupV3Endpoint(endpoint string) (V2Endpoint, bool) {
	base := normalizeEndpoint(endpoint)
	ep, ok := V2EndpointMap[base]
	return ep, ok
}

// curlRangeInfo tracks a range variable's origin for curl target field access.
type curlRangeInfo struct {
	cc    *CliCommandCall
	field string
}

// scanFunctionForCliCommands detects all CliCommand/CliCommandWithoutTerminalOutput calls.
// For "curl" commands, it also traces through json.Unmarshal and tracks field access.
func scanFunctionForCliCommands(fset *token.FileSet, path string, fn *ast.FuncDecl, result *ScanResult) {
	// Phase 1: Find CliCommand calls, string vars, composite literal types, and json.Unmarshal links.
	stringVars := make(map[string]string)           // varName → string literal value
	varTypes := make(map[string]string)              // varName → type name from composite literals
	curlOutputVars := make(map[string]*CliCommandCall) // curl result vars only
	curlTargetVars := make(map[string]*CliCommandCall) // curl unmarshal targets only

	var calls []*CliCommandCall

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch stmt := n.(type) {
		case *ast.AssignStmt:
			// Track string literal assignments: nextURL := "v2/apps"
			if len(stmt.Lhs) == 1 && len(stmt.Rhs) == 1 {
				if ident, ok := stmt.Lhs[0].(*ast.Ident); ok {
					if lit, ok := stmt.Rhs[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						stringVars[ident.Name] = strings.Trim(lit.Value, `"`)
					}
					if typeName := extractCompositeLitType(stmt.Rhs[0]); typeName != "" {
						varTypes[ident.Name] = typeName
					}
				}
			}

			// Check for CliCommand/CliCommandWithoutTerminalOutput call
			if cc := extractCliCommandCall(stmt, fset, path); cc != nil {
				calls = append(calls, cc)

				// Curl-specific: track output var and resolve endpoint variable
				if cc.Command == "curl" {
					if cc.ResultVar != "" {
						curlOutputVars[cc.ResultVar] = cc
					}
					if cc.EndpointVar != "" && cc.Endpoint == "" {
						if val, ok := stringVars[cc.EndpointVar]; ok {
							cc.Endpoint = val
							if v3, ok := lookupV3Endpoint(val); ok {
								cc.V3Endpoint = v3.V3Path
								cc.V3Notes = v3.Notes
							}
						}
					}
				}
				return true
			}

			// Check for json.Unmarshal in assignment: err = json.Unmarshal(...)
			for _, rhs := range stmt.Rhs {
				linkUnmarshal(rhs, curlOutputVars, curlTargetVars, varTypes)
			}

		case *ast.ReturnStmt:
			// Check for CliCommand/CliCommandWithoutTerminalOutput in return statements:
			// return cliConnection.CliCommandWithoutTerminalOutput("curl", url)
			for _, expr := range stmt.Results {
				if cc := extractCliCommandFromExpr(expr, fset, path, stmt.Pos()); cc != nil {
					// Resolve endpoint variable from tracked string literals
					if cc.Command == "curl" && cc.EndpointVar != "" && cc.Endpoint == "" {
						if val, ok := stringVars[cc.EndpointVar]; ok {
							cc.Endpoint = val
							if v3, ok := lookupV3Endpoint(val); ok {
								cc.V3Endpoint = v3.V3Path
								cc.V3Notes = v3.Notes
							}
						}
					}
					calls = append(calls, cc)
				}
			}

		case *ast.ExprStmt:
			// Check for json.Unmarshal as bare expression statement
			linkUnmarshal(stmt.X, curlOutputVars, curlTargetVars, varTypes)
		}
		return true
	})

	if len(calls) == 0 {
		return
	}

	// Phase 2 (curl only): Track field access on target variables and range variables.
	if len(curlTargetVars) > 0 {
		curlRangeVars := make(map[string]curlRangeInfo)

		ast.Inspect(fn.Body, func(n ast.Node) bool {
			// Track range variables over curl target fields: for _, app := range apps.Resources
			if rangeStmt, ok := n.(*ast.RangeStmt); ok {
				if sel, ok := rangeStmt.X.(*ast.SelectorExpr); ok {
					if ident, ok := sel.X.(*ast.Ident); ok {
						if cc, tracked := curlTargetVars[ident.Name]; tracked {
							if rangeStmt.Value != nil {
								if valIdent, ok := rangeStmt.Value.(*ast.Ident); ok {
									curlRangeVars[valIdent.Name] = curlRangeInfo{
										cc:    cc,
										field: sel.Sel.Name,
									}
								}
							}
							// Also record the field being ranged over
							cc.Fields[sel.Sel.Name] = true
						}
					}
				}
				return true
			}

			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}

			chain := resolveSelectorChain(sel)
			if len(chain) < 2 {
				return true
			}

			rootVar := chain[0]

			// Direct access on a target variable: apps.NextURL, apps.Resources
			if cc, ok := curlTargetVars[rootVar]; ok {
				fieldPath := strings.Join(chain[1:], ".")
				cc.Fields[fieldPath] = true
				return false
			}

			// Access on a range variable: app.Entity.Name
			if ri, ok := curlRangeVars[rootVar]; ok {
				subPath := strings.Join(chain[1:], ".")
				fieldPath := ri.field + "." + subPath
				ri.cc.Fields[fieldPath] = true
				return false
			}

			return true
		})
	}

	result.CliCommandCalls = append(result.CliCommandCalls, calls...)
}

// extractCliCommandCall checks if an AssignStmt is a CliCommand/CliCommandWithoutTerminalOutput call.
func extractCliCommandCall(stmt *ast.AssignStmt, fset *token.FileSet, path string) *CliCommandCall {
	if len(stmt.Rhs) != 1 {
		return nil
	}
	cc := extractCliCommandFromExpr(stmt.Rhs[0], fset, path, stmt.Pos())
	if cc == nil {
		return nil
	}

	// Extract result variable from the assignment LHS
	if len(stmt.Lhs) >= 1 {
		if ident, ok := stmt.Lhs[0].(*ast.Ident); ok {
			cc.ResultVar = ident.Name
		}
	}

	return cc
}

// extractCliCommandFromExpr checks if an expression is a CliCommand/CliCommandWithoutTerminalOutput call.
func extractCliCommandFromExpr(expr ast.Expr, fset *token.FileSet, path string, pos token.Pos) *CliCommandCall {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil
	}

	methodName := sel.Sel.Name
	if methodName != "CliCommand" && methodName != "CliCommandWithoutTerminalOutput" {
		return nil
	}

	cc := &CliCommandCall{
		File:   path,
		Line:   fset.Position(pos).Line,
		Method: methodName,
		Fields: make(map[string]bool),
	}

	// Extract the command (first arg) and remaining args
	for i, arg := range call.Args {
		switch a := arg.(type) {
		case *ast.BasicLit:
			if a.Kind == token.STRING {
				val := strings.Trim(a.Value, `"`)
				if i == 0 {
					cc.Command = val
				} else {
					cc.Args = append(cc.Args, val)
				}
			}
		case *ast.Ident:
			if i == 0 {
				cc.Command = "(var: " + a.Name + ")"
			} else {
				cc.Args = append(cc.Args, "(var: "+a.Name+")")
			}
		}
	}

	// Curl-specific: extract endpoint and look up V3 equivalent
	if cc.Command == "curl" && len(call.Args) >= 2 {
		switch arg := call.Args[1].(type) {
		case *ast.BasicLit:
			if arg.Kind == token.STRING {
				cc.Endpoint = strings.Trim(arg.Value, `"`)
			}
		case *ast.Ident:
			cc.EndpointVar = arg.Name
		}

		if cc.Endpoint != "" {
			if v3, ok := lookupV3Endpoint(cc.Endpoint); ok {
				cc.V3Endpoint = v3.V3Path
				cc.V3Notes = v3.Notes
			}
		}
	}

	return cc
}

// linkUnmarshal checks for json.Unmarshal calls and links curl output variables to target variables.
func linkUnmarshal(expr ast.Expr, curlOutputVars, curlTargetVars map[string]*CliCommandCall, varTypes map[string]string) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok || ident.Name != "json" || sel.Sel.Name != "Unmarshal" {
		return
	}
	if len(call.Args) != 2 {
		return
	}

	outputVar := extractOutputVarFromBytes(call.Args[0])
	if outputVar == "" {
		return
	}

	cc, ok := curlOutputVars[outputVar]
	if !ok {
		return
	}

	targetVar := extractAddrTarget(call.Args[1])
	if targetVar == "" {
		return
	}

	cc.TargetVar = targetVar
	if typeName, ok := varTypes[targetVar]; ok {
		cc.TargetType = typeName
	}
	curlTargetVars[targetVar] = cc
}

// extractOutputVarFromBytes extracts the variable name from patterns like:
//   - []byte(output[0])
//   - []byte(strings.Join(output, ""))
func extractOutputVarFromBytes(expr ast.Expr) string {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return ""
	}
	if len(call.Args) != 1 {
		return ""
	}

	arg := call.Args[0]

	// Pattern: output[0]
	if idx, ok := arg.(*ast.IndexExpr); ok {
		if ident, ok := idx.X.(*ast.Ident); ok {
			return ident.Name
		}
	}

	// Pattern: strings.Join(output, "")
	if innerCall, ok := arg.(*ast.CallExpr); ok {
		if sel, ok := innerCall.Fun.(*ast.SelectorExpr); ok {
			if pkg, ok := sel.X.(*ast.Ident); ok {
				if pkg.Name == "strings" && sel.Sel.Name == "Join" && len(innerCall.Args) >= 1 {
					if ident, ok := innerCall.Args[0].(*ast.Ident); ok {
						return ident.Name
					}
				}
			}
		}
	}

	return ""
}

// extractAddrTarget extracts the variable name from &varName.
func extractAddrTarget(expr ast.Expr) string {
	unary, ok := expr.(*ast.UnaryExpr)
	if !ok || unary.Op != token.AND {
		return ""
	}
	ident, ok := unary.X.(*ast.Ident)
	if !ok {
		return ""
	}
	return ident.Name
}

// extractCompositeLitType extracts the type name from a composite literal.
// e.g., AppsModel{} → "AppsModel", pkg.Type{} → "pkg.Type"
func extractCompositeLitType(expr ast.Expr) string {
	comp, ok := expr.(*ast.CompositeLit)
	if !ok {
		return ""
	}
	switch t := comp.Type.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		if pkg, ok := t.X.(*ast.Ident); ok {
			return pkg.Name + "." + t.Sel.Name
		}
	}
	return ""
}
