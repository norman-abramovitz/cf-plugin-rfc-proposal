package generator

import (
	"cf-plugin-migrate/scanner"
)

// GroupDependency defines a dependency between groups within a method.
// If the dependent group is active (has requested fields), the required
// group must also be active even if none of its fields are requested.
type GroupDependency struct {
	Method    string // e.g., "GetApp"
	Dependent int    // group index that depends on another
	Required  int    // group index that must be active
}

// Dependencies defines the static dependency chains between groups.
// These are frozen — the V2 models never change.
var Dependencies = []GroupDependency{
	// GetApp: Stats (idx 2) requires Process (idx 1) for process GUID
	{Method: "GetApp", Dependent: 2, Required: 1},
	// GetApp: Stack (idx 4) requires Droplet (idx 3) for stack name
	{Method: "GetApp", Dependent: 4, Required: 3},
	// GetApps: Stats (idx 2) requires Process (idx 1) for process GUID
	{Method: "GetApps", Dependent: 2, Required: 1},
}

// ResolvedMethod holds the resolved group activation for a method.
type ResolvedMethod struct {
	Name         string
	ActiveGroups []bool   // indexed by group index — true if this group should be generated
	Fields       []string // original requested fields
	SubFields    map[string][]string
}

// ResolveMethod determines which groups are active for a method given
// the requested fields. It activates dependency groups as needed.
func ResolveMethod(method string, mc *MethodConfig) *ResolvedMethod {
	modelInfo := scanner.V2Models[method]
	if modelInfo == nil {
		return nil
	}

	active := make([]bool, len(modelInfo.Groups))

	// Activate groups that have at least one requested field.
	for _, field := range mc.Fields {
		if groupIdx, ok := modelInfo.FieldGroup[field]; ok {
			active[groupIdx] = true
		}
	}

	// Force-activate dependency groups.
	for _, dep := range Dependencies {
		if dep.Method == method && active[dep.Dependent] {
			active[dep.Required] = true
		}
	}

	return &ResolvedMethod{
		Name:         method,
		ActiveGroups: active,
		Fields:       mc.Fields,
		SubFields:    mc.SubFields,
	}
}

// HasField reports whether a specific field is in the requested fields list.
func (rm *ResolvedMethod) HasField(field string) bool {
	return containsField(rm.Fields, field)
}

// HasGroup reports whether a specific group index is active.
func (rm *ResolvedMethod) HasGroup(idx int) bool {
	if idx < 0 || idx >= len(rm.ActiveGroups) {
		return false
	}
	return rm.ActiveGroups[idx]
}

// HasSubField reports whether a specific sub-field key has entries.
func (rm *ResolvedMethod) HasSubField(key string) bool {
	_, ok := rm.SubFields[key]
	return ok
}

// ActiveGroupCount returns the number of active groups.
func (rm *ResolvedMethod) ActiveGroupCount() int {
	count := 0
	for _, a := range rm.ActiveGroups {
		if a {
			count++
		}
	}
	return count
}
