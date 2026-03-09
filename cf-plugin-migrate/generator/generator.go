package generator

import (
	"bytes"
	"fmt"
	"go/format"
	"sort"
	"strings"
	"text/template"

	"cf-plugin-migrate/scanner"
)

// TemplateData is the top-level data passed to the file template.
type TemplateData struct {
	Package      string
	Methods      []*ResolvedMethod // sorted by method order
	HasDomain    bool              // true if any domain methods are present
	NeedsTime    bool              // true if Stats group is active (time.Now for Since)
	NeedsStrings bool              // true if Routes group is active (strings.Index for domain)
}

// methodOrder defines the output order for domain methods.
var methodOrder = []string{
	"GetApp", "GetApps",
	"GetService", "GetServices",
	"GetOrg", "GetOrgs",
	"GetSpace", "GetSpaces",
	"GetOrgUsers", "GetSpaceUsers",
}

// Generate produces Go source code from a GenerateConfig.
// The output implements the V2 CliConnection interface as a V2Compat struct
// with session pass-through and domain methods backed by CAPI V3.
func Generate(config *GenerateConfig) ([]byte, error) {
	// Resolve active groups for each method.
	var methods []*ResolvedMethod
	for _, name := range methodOrder {
		mc, ok := config.Methods[name]
		if !ok {
			continue
		}
		rm := ResolveMethod(name, mc)
		if rm == nil {
			return nil, fmt.Errorf("failed to resolve method %s", name)
		}
		methods = append(methods, rm)
	}

	// Compute conditional import flags based on active groups.
	needsTime := false
	needsStrings := false
	for _, rm := range methods {
		modelInfo := scanner.V2Models[rm.Name]
		if modelInfo == nil {
			continue
		}
		for i, g := range modelInfo.Groups {
			if !rm.HasGroup(i) {
				continue
			}
			if g.Name == "Stats" {
				needsTime = true
			}
			if g.Name == "Routes" {
				needsStrings = true
			}
		}
	}

	data := &TemplateData{
		Package:      config.Package,
		Methods:      methods,
		HasDomain:    len(methods) > 0,
		NeedsTime:    needsTime,
		NeedsStrings: needsStrings,
	}

	// Build and execute template.
	tmpl, err := buildTemplate()
	if err != nil {
		return nil, fmt.Errorf("building template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "file", data); err != nil {
		return nil, fmt.Errorf("executing template: %w", err)
	}

	// Format with go/format for consistent output.
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("formatting generated code: %w\n\nRaw output:\n%s", err, buf.String())
	}

	return formatted, nil
}

// buildTemplate assembles all templates with helper functions.
func buildTemplate() (*template.Template, error) {
	funcMap := template.FuncMap{
		"hasField":    func(rm *ResolvedMethod, field string) bool { return rm.HasField(field) },
		"hasGroup":    func(rm *ResolvedMethod, idx int) bool { return rm.HasGroup(idx) },
		"hasSubField": func(rm *ResolvedMethod, key string) bool { return rm.HasSubField(key) },
		"groupCount":  func(rm *ResolvedMethod) int { return rm.ActiveGroupCount() },
		"apiCalls":    apiCallSummary,
		"join":        strings.Join,
		"modelInfo":   func(method string) *scanner.ModelInfo { return scanner.V2Models[method] },
		"hasMethod": func(data *TemplateData, name string) bool {
			for _, m := range data.Methods {
				if m.Name == name {
					return true
				}
			}
			return false
		},
	}

	tmpl := template.New("").Funcs(funcMap)

	// Parse embedded templates.
	for name, content := range embeddedTemplates() {
		if _, err := tmpl.New(name).Parse(content); err != nil {
			return nil, fmt.Errorf("parsing template %s: %w", name, err)
		}
	}

	return tmpl, nil
}

// apiCallSummary returns a human-readable summary of V3 API calls for a method.
func apiCallSummary(rm *ResolvedMethod) string {
	modelInfo := scanner.V2Models[rm.Name]
	if modelInfo == nil {
		return ""
	}

	var calls []string
	for i, g := range modelInfo.Groups {
		if rm.HasGroup(i) {
			calls = append(calls, g.APICall)
		}
	}

	count := len(calls)
	suffix := "call"
	if count != 1 {
		suffix = "calls"
	}

	return fmt.Sprintf("%s (%d %s)", strings.Join(calls, ", "), count, suffix)
}

// MethodNames returns the sorted list of method names from the config.
func MethodNames(config *GenerateConfig) []string {
	names := make([]string, 0, len(config.Methods))
	for name := range config.Methods {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
