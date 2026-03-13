package scanner

import (
	"go/ast"
	"go/token"
	"strings"
)

// DiscoveredEndpoint represents an API endpoint string literal found in the source code.
type DiscoveredEndpoint struct {
	Endpoint string // the URL string literal (may contain format verbs like %v)
	File     string
	Line     int
	Function string // containing function name

	// Sink is the function the URL variable is passed to (e.g., "NewCommonV2ResponseManager").
	// Empty if the literal is used inline or not passed to a function.
	Sink string

	V3Endpoint string // suggested V3 equivalent
	V3Notes    string

	// Traced is true if this endpoint was also found by cross-function parameter tracing.
	Traced bool
}

// discoverEndpoints walks all parsed files and finds string literals that look like
// CAPI V2 or V3 API endpoint paths. This provides a complete inventory of API URLs
// referenced in the source code, independent of call-chain tracing.
//
// First pass uses simple /v2/ and /v3/ prefix matching. If CliCommand calls exist
// but no endpoints were discovered, a second pass uses known CAPI resource names
// to catch parameterized version strings like "v%s/apps".
func discoverEndpoints(files []*parsedFile, result *ScanResult) {
	// Build a set of resolved endpoints for cross-referencing.
	resolvedSet := make(map[string]bool)
	for _, cc := range result.CliCommandCalls {
		if cc.Endpoint != "" {
			resolvedSet[cc.Endpoint] = true
		}
		for _, ep := range cc.ResolvedEndpoints {
			resolvedSet[ep.Endpoint] = true
		}
	}

	// First pass: match /v2/ and /v3/ prefixes.
	scanEndpointLiterals(files, resolvedSet, result, isAPIEndpointSimple)

	// If we have CliCommand calls but found nothing, try deeper resource-name matching.
	if len(result.DiscoveredEndpoints) == 0 && len(result.CliCommandCalls) > 0 {
		scanEndpointLiterals(files, resolvedSet, result, isAPIEndpointDeep)
	}
}

func scanEndpointLiterals(files []*parsedFile, resolvedSet map[string]bool, result *ScanResult, matcher func(string) bool) {
	for _, pf := range files {
		for _, decl := range pf.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			discoverEndpointsInFunc(pf, fn, resolvedSet, result, matcher)
		}
	}
}

// discoverEndpointsInFunc scans a function body for string literals containing
// /v2/ or /v3/ API paths, and tracks when they're passed to function calls.
func discoverEndpointsInFunc(pf *parsedFile, fn *ast.FuncDecl, resolvedSet map[string]bool, result *ScanResult, matcher func(string) bool) {
	// Track string variable assignments: url := "/v2/apps"
	type urlVar struct {
		value string
		pos   token.Pos
	}
	urlVars := make(map[string]urlVar) // varName → URL info

	// Deduplicate: only report each endpoint once per function.
	seen := make(map[string]bool) // endpoint value → already reported

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if assign, ok := n.(*ast.AssignStmt); ok {
			if len(assign.Lhs) == 1 && len(assign.Rhs) == 1 {
				if ident, ok := assign.Lhs[0].(*ast.Ident); ok {
					// Direct string literal: url := "/v2/apps"
					if lit, ok := assign.Rhs[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						val := strings.Trim(lit.Value, `"`)
						if matcher(val) {
							urlVars[ident.Name] = urlVar{value: val, pos: lit.Pos()}
						}
					}
					// fmt.Sprintf: url := fmt.Sprintf("/v2/routes/%v/apps", routeId)
					if val, pos, ok := extractFmtSprintf(assign.Rhs[0]); ok {
						if matcher(val) {
							urlVars[ident.Name] = urlVar{value: val, pos: pos}
						}
					}
					// String concatenation: url := "/v2/apps/" + appId + "/stats"
					if val, pos, ok := extractStringConcat(assign.Rhs[0]); ok {
						if matcher(val) {
							urlVars[ident.Name] = urlVar{value: val, pos: pos}
						}
					}
				}
			}
		}

		// Look for function calls that receive a URL variable as an argument.
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		for _, arg := range call.Args {
			switch a := arg.(type) {
			case *ast.BasicLit:
				// Inline string literal passed directly to a function call.
				if a.Kind == token.STRING {
					val := strings.Trim(a.Value, `"`)
					if matcher(val) && !seen[val] {
						seen[val] = true
						pos := pf.fset.Position(a.Pos())
						callName := extractCallName(call)
						ep := DiscoveredEndpoint{
							Endpoint: val,
							File:     pf.path,
							Line:     pos.Line,
							Function: methodKey(fn),
							Sink:     callName,
							Traced:   resolvedSet[val],
						}
						if v3, ok := lookupV3Endpoint(val); ok {
							ep.V3Endpoint = v3.V3Path
							ep.V3Notes = v3.Notes
						}
						result.DiscoveredEndpoints = append(result.DiscoveredEndpoints, &ep)
					}
				}
			case *ast.Ident:
				// Variable passed to a function call — check if it's a tracked URL.
				if uv, ok := urlVars[a.Name]; ok && !seen[uv.value] {
					seen[uv.value] = true
					pos := pf.fset.Position(call.Pos())
					callName := extractCallName(call)
					ep := DiscoveredEndpoint{
						Endpoint: uv.value,
						File:     pf.path,
						Line:     pos.Line,
						Function: methodKey(fn),
						Sink:     callName,
						Traced:   resolvedSet[uv.value],
					}
					if v3, ok := lookupV3Endpoint(uv.value); ok {
						ep.V3Endpoint = v3.V3Path
						ep.V3Notes = v3.Notes
					}
					result.DiscoveredEndpoints = append(result.DiscoveredEndpoints, &ep)
					// Remove from urlVars so we don't double-report it
					// from the standalone check below.
					delete(urlVars, a.Name)
				}
			}
		}
		return true
	})

	// Report any URL variables that were assigned but never passed to a function call
	// (they might be used in string concatenation, struct fields, etc.)
	for _, uv := range urlVars {
		if seen[uv.value] {
			continue
		}
		pos := pf.fset.Position(uv.pos)
		ep := DiscoveredEndpoint{
			Endpoint: uv.value,
			File:     pf.path,
			Line:     pos.Line,
			Function: methodKey(fn),
			Traced:   resolvedSet[uv.value],
		}
		if v3, ok := lookupV3Endpoint(uv.value); ok {
			ep.V3Endpoint = v3.V3Path
			ep.V3Notes = v3.Notes
		}
		result.DiscoveredEndpoints = append(result.DiscoveredEndpoints, &ep)
	}
}

// isAPIEndpointSimple checks for the common case: string contains /v2/ or /v3/
// or starts with v2/ or v3/.
func isAPIEndpointSimple(s string) bool {
	return strings.Contains(s, "/v2/") || strings.HasPrefix(s, "v2/") ||
		strings.Contains(s, "/v3/") || strings.HasPrefix(s, "v3/")
}

// isAPIEndpointDeep matches known CAPI resource names after any version-like prefix,
// catching parameterized patterns like "v%s/apps" or "v%v/spaces".
func isAPIEndpointDeep(s string) bool {
	if isAPIEndpointSimple(s) {
		return true
	}

	s = strings.TrimPrefix(s, "/")
	parts := strings.Split(s, "/")
	for i, part := range parts {
		if isVersionSegment(part) && i+1 < len(parts) {
			if capiResources[parts[i+1]] {
				return true
			}
		}
	}
	return false
}

// capiResources is the set of known CAPI resource names used by isAPIEndpointDeep
// to identify API endpoints with parameterized version strings.
var capiResources map[string]bool

func init() {
	capiResources = make(map[string]bool)

	// Extract resource names from V2EndpointMap (e.g., "v2/apps" → "apps").
	for key := range V2EndpointMap {
		parts := strings.SplitN(key, "/", 2)
		if len(parts) == 2 {
			capiResources[parts[1]] = true
		}
	}

	// V3-only resources not in the V2 map.
	for _, r := range []string{
		"isolation_segments",
		"service_offerings",
		"service_credential_bindings",
		"organization_quotas",
		"space_quotas",
		"audit_events",
		"roles",
		"deployments",
		"processes",
		"tasks",
		"packages",
		"droplets",
		"builds",
		"revisions",
		"sidecars",
		"feature_flags",
		"resource_matches",
		"app_features",
		"service_route_bindings",
		"service_brokers",
		"service_usage_events",
		"app_usage_events",
		"jobs",
	} {
		capiResources[r] = true
	}
}

// extractStringConcat extracts an API endpoint from string concatenation expressions.
// Handles patterns like: "/v2/apps/" + appId + "/stats" → "/v2/apps/..."
// Collects all string literal parts and joins them with "..." for non-literal parts.
func extractStringConcat(expr ast.Expr) (string, token.Pos, bool) {
	bin, ok := expr.(*ast.BinaryExpr)
	if !ok || bin.Op != token.ADD {
		return "", 0, false
	}

	var parts []string
	var firstPos token.Pos
	collectConcatParts(bin, &parts, &firstPos)

	if len(parts) == 0 {
		return "", 0, false
	}

	result := strings.Join(parts, "")
	return result, firstPos, true
}

// collectConcatParts walks a binary ADD tree and collects string literals,
// replacing non-literal operands with "..." as a placeholder.
func collectConcatParts(expr ast.Expr, parts *[]string, firstPos *token.Pos) {
	if bin, ok := expr.(*ast.BinaryExpr); ok && bin.Op == token.ADD {
		collectConcatParts(bin.X, parts, firstPos)
		collectConcatParts(bin.Y, parts, firstPos)
		return
	}
	if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		val := strings.Trim(lit.Value, `"`)
		if *firstPos == 0 {
			*firstPos = lit.Pos()
		}
		*parts = append(*parts, val)
	} else {
		*parts = append(*parts, "...")
	}
}

// isVersionSegment returns true if the path segment looks like a CAPI version
// prefix: "v2", "v3", or a parameterized variant like "v%s", "v%v", "v%d".
func isVersionSegment(s string) bool {
	if len(s) < 2 || s[0] != 'v' {
		return false
	}
	rest := s[1:]
	if rest == "2" || rest == "3" {
		return true
	}
	if strings.HasPrefix(rest, "%") {
		return true
	}
	return false
}
