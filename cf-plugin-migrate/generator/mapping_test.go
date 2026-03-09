package generator

import (
	"testing"
)

func TestResolveMethod_SimpleFields(t *testing.T) {
	mc := &MethodConfig{
		Fields:    []string{"Name", "Guid"},
		SubFields: make(map[string][]string),
	}
	rm := ResolveMethod("GetApp", mc)
	if rm == nil {
		t.Fatal("expected non-nil ResolvedMethod")
	}

	// Group 0 (App) should be active — Name and Guid are in group 0.
	if !rm.HasGroup(0) {
		t.Error("expected group 0 (App) to be active")
	}

	// No other groups should be active.
	for i := 1; i < len(rm.ActiveGroups); i++ {
		if rm.HasGroup(i) {
			t.Errorf("expected group %d to be inactive", i)
		}
	}

	if rm.ActiveGroupCount() != 1 {
		t.Errorf("ActiveGroupCount = %d, want 1", rm.ActiveGroupCount())
	}
}

func TestResolveMethod_StatsRequiresProcess(t *testing.T) {
	// GetApp: Stats (group 2) depends on Process (group 1).
	mc := &MethodConfig{
		Fields:    []string{"Guid", "RunningInstances"},
		SubFields: make(map[string][]string),
	}
	rm := ResolveMethod("GetApp", mc)

	// Group 0 (App) active — Guid
	if !rm.HasGroup(0) {
		t.Error("expected group 0 (App) to be active")
	}
	// Group 1 (Process) force-activated by dependency
	if !rm.HasGroup(1) {
		t.Error("expected group 1 (Process) to be force-activated")
	}
	// Group 2 (Stats) active — RunningInstances
	if !rm.HasGroup(2) {
		t.Error("expected group 2 (Stats) to be active")
	}

	if rm.ActiveGroupCount() != 3 {
		t.Errorf("ActiveGroupCount = %d, want 3", rm.ActiveGroupCount())
	}
}

func TestResolveMethod_StackRequiresDroplet(t *testing.T) {
	// GetApp: Stack (group 4) depends on Droplet (group 3).
	mc := &MethodConfig{
		Fields:    []string{"Guid", "Stack"},
		SubFields: make(map[string][]string),
	}
	rm := ResolveMethod("GetApp", mc)

	// Group 0 (App) active — Guid
	if !rm.HasGroup(0) {
		t.Error("expected group 0 (App) to be active")
	}
	// Group 3 (Droplet) force-activated by dependency
	if !rm.HasGroup(3) {
		t.Error("expected group 3 (Droplet) to be force-activated")
	}
	// Group 4 (Stack) active — Stack
	if !rm.HasGroup(4) {
		t.Error("expected group 4 (Stack) to be active")
	}

	if rm.ActiveGroupCount() != 3 {
		t.Errorf("ActiveGroupCount = %d, want 3", rm.ActiveGroupCount())
	}
}

func TestResolveMethod_GetAppsStatsRequiresProcess(t *testing.T) {
	// GetApps: Stats (group 2) depends on Process (group 1).
	mc := &MethodConfig{
		Fields:    []string{"Guid", "RunningInstances"},
		SubFields: make(map[string][]string),
	}
	rm := ResolveMethod("GetApps", mc)

	if !rm.HasGroup(0) {
		t.Error("expected group 0 (Apps) to be active")
	}
	if !rm.HasGroup(1) {
		t.Error("expected group 1 (Process) to be force-activated")
	}
	if !rm.HasGroup(2) {
		t.Error("expected group 2 (Stats) to be active")
	}
}

func TestResolveMethod_NoDependencyWhenNotNeeded(t *testing.T) {
	// GetApp: Process fields without Stats — no dependency forcing.
	mc := &MethodConfig{
		Fields:    []string{"Guid", "Command", "Memory"},
		SubFields: make(map[string][]string),
	}
	rm := ResolveMethod("GetApp", mc)

	if !rm.HasGroup(0) {
		t.Error("expected group 0 (App) to be active")
	}
	if !rm.HasGroup(1) {
		t.Error("expected group 1 (Process) to be active")
	}
	// Stats should NOT be active.
	if rm.HasGroup(2) {
		t.Error("expected group 2 (Stats) to be inactive")
	}
}

func TestResolveMethod_HasField(t *testing.T) {
	mc := &MethodConfig{
		Fields:    []string{"Name", "Guid", "Routes"},
		SubFields: make(map[string][]string),
	}
	rm := ResolveMethod("GetApp", mc)

	if !rm.HasField("Name") {
		t.Error("expected HasField(Name) = true")
	}
	if !rm.HasField("Routes") {
		t.Error("expected HasField(Routes) = true")
	}
	if rm.HasField("State") {
		t.Error("expected HasField(State) = false")
	}
}

func TestResolveMethod_HasSubField(t *testing.T) {
	mc := &MethodConfig{
		Fields:    []string{"Name", "Guid", "Routes"},
		SubFields: map[string][]string{"route_fields": {"Host", "Domain.Name"}},
	}
	rm := ResolveMethod("GetApp", mc)

	if !rm.HasSubField("route_fields") {
		t.Error("expected HasSubField(route_fields) = true")
	}
	if rm.HasSubField("service_fields") {
		t.Error("expected HasSubField(service_fields) = false")
	}
}

func TestResolveMethod_UnknownMethod(t *testing.T) {
	mc := &MethodConfig{
		Fields:    []string{"Name"},
		SubFields: make(map[string][]string),
	}
	rm := ResolveMethod("GetFoo", mc)
	if rm != nil {
		t.Error("expected nil for unknown method")
	}
}

func TestResolveMethod_HasGroupOutOfBounds(t *testing.T) {
	mc := &MethodConfig{
		Fields:    []string{"Guid"},
		SubFields: make(map[string][]string),
	}
	rm := ResolveMethod("GetOrgs", mc)

	if rm.HasGroup(-1) {
		t.Error("expected HasGroup(-1) = false")
	}
	if rm.HasGroup(100) {
		t.Error("expected HasGroup(100) = false")
	}
}
