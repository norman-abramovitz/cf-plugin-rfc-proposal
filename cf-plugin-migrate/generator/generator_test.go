package generator

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// TestGenerate_NoDomainMethods verifies the output for a session-only config.
func TestGenerate_NoDomainMethods(t *testing.T) {
	config := &GenerateConfig{
		SchemaVersion: "1",
		Package:       "main",
		Methods:       map[string]*MethodConfig{},
	}

	output, err := Generate(config)
	if err != nil {
		t.Fatal(err)
	}

	src := string(output)

	// Should not import go-cfclient or context.
	if strings.Contains(src, `"github.com/cloudfoundry/go-cfclient`) {
		t.Error("unexpected go-cfclient import for session-only config")
	}
	if strings.Contains(src, `"context"`) {
		t.Error("unexpected context import for session-only config")
	}

	// Struct should not have cfClient field.
	if strings.Contains(src, "cfClient") {
		t.Error("unexpected cfClient field for session-only config")
	}

	// All domain methods should pass through.
	if !strings.Contains(src, "c.conn.GetApp(appName)") {
		t.Error("expected GetApp pass-through")
	}
	if !strings.Contains(src, "c.conn.GetApps()") {
		t.Error("expected GetApps pass-through")
	}

	// Should compile.
	assertCompiles(t, src)
}

// TestGenerate_WithDomainMethods verifies the output for a config with domain methods.
func TestGenerate_WithDomainMethods(t *testing.T) {
	config := &GenerateConfig{
		SchemaVersion: "1",
		Package:       "main",
		Methods: map[string]*MethodConfig{
			"GetApp": {
				Fields:    []string{"Name", "Guid"},
				SubFields: make(map[string][]string),
			},
			"GetApps": {
				Fields:    []string{"Name", "Guid"},
				SubFields: make(map[string][]string),
			},
		},
	}

	output, err := Generate(config)
	if err != nil {
		t.Fatal(err)
	}

	src := string(output)

	// Should import go-cfclient and context.
	if !strings.Contains(src, `"github.com/cloudfoundry/go-cfclient`) {
		t.Error("expected go-cfclient import")
	}
	if !strings.Contains(src, `"context"`) {
		t.Error("expected context import")
	}

	// GetApp and GetApps should dispatch to internal methods.
	if !strings.Contains(src, "c.getApp(appName)") {
		t.Error("expected GetApp dispatch to c.getApp")
	}
	if !strings.Contains(src, "c.getApps()") {
		t.Error("expected GetApps dispatch to c.getApps")
	}

	// GetOrg should still pass through.
	if !strings.Contains(src, "c.conn.GetOrg(orgName)") {
		t.Error("expected GetOrg pass-through")
	}

	// Should compile.
	assertCompiles(t, src)
}

// TestGenerate_APICallSummary verifies the header comments show correct API calls.
func TestGenerate_APICallSummary(t *testing.T) {
	config := &GenerateConfig{
		SchemaVersion: "1",
		Package:       "main",
		Methods: map[string]*MethodConfig{
			"GetApp": {
				Fields:    []string{"Name", "Guid", "Routes"},
				SubFields: make(map[string][]string),
			},
		},
	}

	output, err := Generate(config)
	if err != nil {
		t.Fatal(err)
	}

	src := string(output)

	// Should mention Applications.Single and Routes.ListForApp in header.
	if !strings.Contains(src, "Applications.Single") {
		t.Error("expected Applications.Single in API call summary")
	}
	if !strings.Contains(src, "Routes.ListForApp") {
		t.Error("expected Routes.ListForApp in API call summary")
	}
	if !strings.Contains(src, "2 calls") {
		t.Error("expected '2 calls' in API call summary")
	}
}

// TestGenerate_PackageName verifies the package name is set correctly.
func TestGenerate_PackageName(t *testing.T) {
	config := &GenerateConfig{
		SchemaVersion: "1",
		Package:       "command",
		Methods:       map[string]*MethodConfig{},
	}

	output, err := Generate(config)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(output), "package command") {
		t.Error("expected 'package command' in output")
	}
}

// TestGenerate_DependencyChainInHeader verifies forced groups appear in the header.
func TestGenerate_DependencyChainInHeader(t *testing.T) {
	config := &GenerateConfig{
		SchemaVersion: "1",
		Package:       "main",
		Methods: map[string]*MethodConfig{
			"GetApp": {
				Fields:    []string{"Guid", "RunningInstances"},
				SubFields: make(map[string][]string),
			},
		},
	}

	output, err := Generate(config)
	if err != nil {
		t.Fatal(err)
	}

	src := string(output)

	// Stats depends on Process — both should appear.
	if !strings.Contains(src, "Applications.Single") {
		t.Error("expected Applications.Single (App group)")
	}
	if !strings.Contains(src, "Processes.ListForApp") {
		t.Error("expected Processes.ListForApp (Process group, force-activated)")
	}
	if !strings.Contains(src, "Processes.GetStats") {
		t.Error("expected Processes.GetStats (Stats group)")
	}
	if !strings.Contains(src, "3 calls") {
		t.Error("expected '3 calls' in API call summary")
	}
}

// TestGenerate_GetOrgsGetSpaces verifies domain method templates produce correct code.
func TestGenerate_GetOrgsGetSpaces(t *testing.T) {
	config := &GenerateConfig{
		SchemaVersion: "1",
		Package:       "main",
		Methods: map[string]*MethodConfig{
			"GetOrgs": {
				Fields:    []string{"Guid", "Name"},
				SubFields: make(map[string][]string),
			},
			"GetSpaces": {
				Fields:    []string{"Guid", "Name"},
				SubFields: make(map[string][]string),
			},
		},
	}

	output, err := Generate(config)
	if err != nil {
		t.Fatal(err)
	}

	src := string(output)

	// GetOrgs should dispatch to c.getOrgs()
	if !strings.Contains(src, "c.getOrgs()") {
		t.Error("expected GetOrgs dispatch to c.getOrgs()")
	}

	// getOrgs implementation should call Organizations.ListAll
	if !strings.Contains(src, "Organizations.ListAll") {
		t.Error("expected Organizations.ListAll call")
	}

	// GetSpaces should dispatch to c.getSpaces()
	if !strings.Contains(src, "c.getSpaces()") {
		t.Error("expected GetSpaces dispatch to c.getSpaces()")
	}

	// getSpaces implementation should call Spaces.ListAll
	if !strings.Contains(src, "Spaces.ListAll") {
		t.Error("expected Spaces.ListAll call")
	}

	// Field assignments should be present
	if !strings.Contains(src, "org.GUID") {
		t.Error("expected org.GUID field assignment")
	}
	if !strings.Contains(src, "space.GUID") {
		t.Error("expected space.GUID field assignment")
	}

	// Unconfigured methods should pass through
	if !strings.Contains(src, "c.conn.GetApp(appName)") {
		t.Error("expected GetApp pass-through")
	}

	assertCompiles(t, src)
}

// TestGenerate_GetOrgsGuidOnly verifies conditional field generation.
func TestGenerate_GetOrgsGuidOnly(t *testing.T) {
	config := &GenerateConfig{
		SchemaVersion: "1",
		Package:       "main",
		Methods: map[string]*MethodConfig{
			"GetOrgs": {
				Fields:    []string{"Guid"},
				SubFields: make(map[string][]string),
			},
		},
	}

	output, err := Generate(config)
	if err != nil {
		t.Fatal(err)
	}

	src := string(output)

	// Should have Guid assignment but not Name
	if !strings.Contains(src, "org.GUID") {
		t.Error("expected org.GUID field assignment")
	}
	if strings.Contains(src, "org.Name") {
		t.Error("unexpected org.Name field assignment when only Guid requested")
	}

	assertCompiles(t, src)
}

// TestGenerate_GetOrg verifies the GetOrg domain method template.
func TestGenerate_GetOrg(t *testing.T) {
	config := &GenerateConfig{
		SchemaVersion: "1",
		Package:       "main",
		Methods: map[string]*MethodConfig{
			"GetOrg": {
				Fields:    []string{"Guid", "Name", "Spaces", "Domains"},
				SubFields: make(map[string][]string),
			},
		},
	}

	output, err := Generate(config)
	if err != nil {
		t.Fatal(err)
	}

	src := string(output)

	// GetOrg should dispatch to c.getOrg(orgName)
	if !strings.Contains(src, "c.getOrg(orgName)") {
		t.Error("expected GetOrg dispatch to c.getOrg(orgName)")
	}

	// Should use Organizations.Single
	if !strings.Contains(src, "Organizations.Single") {
		t.Error("expected Organizations.Single call")
	}

	// Should have Spaces group (Spaces.ListAll)
	if !strings.Contains(src, "Spaces.ListAll") {
		t.Error("expected Spaces.ListAll call for Spaces group")
	}

	// Should have Domains group (Domains.ListForOrganizationAll)
	if !strings.Contains(src, "Domains.ListForOrganizationAll") {
		t.Error("expected Domains.ListForOrganizationAll call for Domains group")
	}

	// Unconfigured methods should pass through
	if !strings.Contains(src, "c.conn.GetApp(appName)") {
		t.Error("expected GetApp pass-through")
	}

	assertCompiles(t, src)
}

// TestGenerate_GetOrgGuidOnly verifies conditional field/group generation for GetOrg.
func TestGenerate_GetOrgGuidOnly(t *testing.T) {
	config := &GenerateConfig{
		SchemaVersion: "1",
		Package:       "main",
		Methods: map[string]*MethodConfig{
			"GetOrg": {
				Fields:    []string{"Guid"},
				SubFields: make(map[string][]string),
			},
		},
	}

	output, err := Generate(config)
	if err != nil {
		t.Fatal(err)
	}

	src := string(output)

	// Should NOT include Spaces, Domains, or SpaceQuotas groups
	if strings.Contains(src, "Spaces.ListAll") {
		t.Error("unexpected Spaces.ListAll when only Guid requested")
	}
	if strings.Contains(src, "Domains.ListForOrganizationAll") {
		t.Error("unexpected Domains.ListForOrganizationAll when only Guid requested")
	}
	if strings.Contains(src, "SpaceQuotas.ListAll") {
		t.Error("unexpected SpaceQuotas.ListAll when only Guid requested")
	}

	assertCompiles(t, src)
}

// TestGenerate_GetSpace verifies the GetSpace domain method template.
func TestGenerate_GetSpace(t *testing.T) {
	config := &GenerateConfig{
		SchemaVersion: "1",
		Package:       "main",
		Methods: map[string]*MethodConfig{
			"GetSpace": {
				Fields:    []string{"Guid", "Name", "Organization", "Applications"},
				SubFields: make(map[string][]string),
			},
		},
	}

	output, err := Generate(config)
	if err != nil {
		t.Fatal(err)
	}

	src := string(output)

	// GetSpace should dispatch to c.getSpace(spaceName)
	if !strings.Contains(src, "c.getSpace(spaceName)") {
		t.Error("expected GetSpace dispatch to c.getSpace(spaceName)")
	}

	// Should use Spaces.Single
	if !strings.Contains(src, "Spaces.Single") {
		t.Error("expected Spaces.Single call")
	}

	// Should have Organization field lookup
	if !strings.Contains(src, "Organizations.Get") {
		t.Error("expected Organizations.Get call for Organization field")
	}

	// Should have Applications group
	if !strings.Contains(src, "Applications.ListAll") {
		t.Error("expected Applications.ListAll for Applications group")
	}

	assertCompiles(t, src)
}

// TestGenerate_GetService verifies the GetService domain method template.
func TestGenerate_GetService(t *testing.T) {
	config := &GenerateConfig{
		SchemaVersion: "1",
		Package:       "main",
		Methods: map[string]*MethodConfig{
			"GetService": {
				Fields:    []string{"Guid", "Name", "ServicePlan", "ServiceOffering"},
				SubFields: make(map[string][]string),
			},
		},
	}

	output, err := Generate(config)
	if err != nil {
		t.Fatal(err)
	}

	src := string(output)

	// GetService should dispatch to c.getService(serviceInstance)
	if !strings.Contains(src, "c.getService(serviceInstance)") {
		t.Error("expected GetService dispatch to c.getService(serviceInstance)")
	}

	// Should use ServiceInstances.Single
	if !strings.Contains(src, "ServiceInstances.Single") {
		t.Error("expected ServiceInstances.Single call")
	}

	// Should have ServicePlan lookup
	if !strings.Contains(src, "ServicePlans.Get") {
		t.Error("expected ServicePlans.Get call")
	}

	// Should have ServiceOffering lookup
	if !strings.Contains(src, "ServiceOfferings.Get") {
		t.Error("expected ServiceOfferings.Get call")
	}

	assertCompiles(t, src)
}

// TestGenerate_GetServiceGuidOnly verifies GetService without chained lookups.
func TestGenerate_GetServiceGuidOnly(t *testing.T) {
	config := &GenerateConfig{
		SchemaVersion: "1",
		Package:       "main",
		Methods: map[string]*MethodConfig{
			"GetService": {
				Fields:    []string{"Guid", "Name"},
				SubFields: make(map[string][]string),
			},
		},
	}

	output, err := Generate(config)
	if err != nil {
		t.Fatal(err)
	}

	src := string(output)

	// Should NOT have ServicePlan or ServiceOffering lookups
	if strings.Contains(src, "ServicePlans.Get") {
		t.Error("unexpected ServicePlans.Get when only Guid+Name requested")
	}
	if strings.Contains(src, "ServiceOfferings.Get") {
		t.Error("unexpected ServiceOfferings.Get when only Guid+Name requested")
	}

	assertCompiles(t, src)
}

// TestGenerate_GetServices verifies the GetServices domain method template.
func TestGenerate_GetServices(t *testing.T) {
	config := &GenerateConfig{
		SchemaVersion: "1",
		Package:       "main",
		Methods: map[string]*MethodConfig{
			"GetServices": {
				Fields:    []string{"Guid", "Name", "ApplicationNames"},
				SubFields: make(map[string][]string),
			},
		},
	}

	output, err := Generate(config)
	if err != nil {
		t.Fatal(err)
	}

	src := string(output)

	// GetServices should dispatch to c.getServices()
	if !strings.Contains(src, "c.getServices()") {
		t.Error("expected GetServices dispatch")
	}

	// Should use ServiceInstances.ListAll
	if !strings.Contains(src, "ServiceInstances.ListAll") {
		t.Error("expected ServiceInstances.ListAll call")
	}

	// Should have ApplicationNames lookup
	if !strings.Contains(src, "ServiceCredentialBindings.ListIncludeAppsAll") {
		t.Error("expected ServiceCredentialBindings.ListIncludeAppsAll call")
	}

	assertCompiles(t, src)
}

// TestGenerate_GetOrgUsers verifies the GetOrgUsers domain method template.
func TestGenerate_GetOrgUsers(t *testing.T) {
	config := &GenerateConfig{
		SchemaVersion: "1",
		Package:       "main",
		Methods: map[string]*MethodConfig{
			"GetOrgUsers": {
				Fields:    []string{"Guid", "Username", "Roles"},
				SubFields: make(map[string][]string),
			},
		},
	}

	output, err := Generate(config)
	if err != nil {
		t.Fatal(err)
	}

	src := string(output)

	// GetOrgUsers should dispatch to c.getOrgUsers(orgName, args...)
	if !strings.Contains(src, "c.getOrgUsers(orgName, args...)") {
		t.Error("expected GetOrgUsers dispatch")
	}

	// Should use Organizations.Single then Roles.ListIncludeUsersAll
	if !strings.Contains(src, "Organizations.Single") {
		t.Error("expected Organizations.Single call")
	}
	if !strings.Contains(src, "Roles.ListIncludeUsersAll") {
		t.Error("expected Roles.ListIncludeUsersAll call")
	}

	assertCompiles(t, src)
}

// TestGenerate_GetSpaceUsers verifies the GetSpaceUsers domain method template.
func TestGenerate_GetSpaceUsers(t *testing.T) {
	config := &GenerateConfig{
		SchemaVersion: "1",
		Package:       "main",
		Methods: map[string]*MethodConfig{
			"GetSpaceUsers": {
				Fields:    []string{"Guid", "Username", "Roles"},
				SubFields: make(map[string][]string),
			},
		},
	}

	output, err := Generate(config)
	if err != nil {
		t.Fatal(err)
	}

	src := string(output)

	// GetSpaceUsers should dispatch to c.getSpaceUsers(orgName, spaceName)
	if !strings.Contains(src, "c.getSpaceUsers(orgName, spaceName)") {
		t.Error("expected GetSpaceUsers dispatch")
	}

	// Should use Organizations.Single, Spaces.Single, and Roles.ListIncludeUsersAll
	if !strings.Contains(src, "Organizations.Single") {
		t.Error("expected Organizations.Single call")
	}
	if !strings.Contains(src, "Spaces.Single") {
		t.Error("expected Spaces.Single call")
	}
	if !strings.Contains(src, "Roles.ListIncludeUsersAll") {
		t.Error("expected Roles.ListIncludeUsersAll call")
	}

	assertCompiles(t, src)
}

// TestGenerate_GetAppGuidOnly verifies the simplest GetApp case.
func TestGenerate_GetAppGuidOnly(t *testing.T) {
	config := &GenerateConfig{
		SchemaVersion: "1",
		Package:       "main",
		Methods: map[string]*MethodConfig{
			"GetApp": {
				Fields:    []string{"Guid"},
				SubFields: make(map[string][]string),
			},
		},
	}

	output, err := Generate(config)
	if err != nil {
		t.Fatal(err)
	}

	src := string(output)

	// Should dispatch to c.getApp(appName)
	if !strings.Contains(src, "c.getApp(appName)") {
		t.Error("expected GetApp dispatch to c.getApp(appName)")
	}

	// Should use Applications.Single
	if !strings.Contains(src, "Applications.Single") {
		t.Error("expected Applications.Single call")
	}

	// Should NOT include process, stats, droplet, routes, etc.
	if strings.Contains(src, "Processes.ListForApp") {
		t.Error("unexpected Process group when only Guid requested")
	}
	if strings.Contains(src, "Droplets.GetCurrentForApp") {
		t.Error("unexpected Droplet group when only Guid requested")
	}
	if strings.Contains(src, "Routes.ListForAppAll") {
		t.Error("unexpected Routes group when only Guid requested")
	}

	assertCompiles(t, src)
}

// TestGenerate_GetAppWithProcess verifies GetApp with process fields.
func TestGenerate_GetAppWithProcess(t *testing.T) {
	config := &GenerateConfig{
		SchemaVersion: "1",
		Package:       "main",
		Methods: map[string]*MethodConfig{
			"GetApp": {
				Fields:    []string{"Guid", "Name", "Command", "Memory", "DiskQuota"},
				SubFields: make(map[string][]string),
			},
		},
	}

	output, err := Generate(config)
	if err != nil {
		t.Fatal(err)
	}

	src := string(output)

	// Should have Process group calls.
	if !strings.Contains(src, "Processes.ListForApp") {
		t.Error("expected Processes.ListForApp call")
	}
	if !strings.Contains(src, "Processes.Get") {
		t.Error("expected Processes.Get call")
	}

	// Should NOT have Stats group (no RunningInstances or Instances requested).
	if strings.Contains(src, "Processes.GetStats") {
		t.Error("unexpected Processes.GetStats when no stats fields requested")
	}

	assertCompiles(t, src)
}

// TestGenerate_GetAppWithStats verifies GetApp with stats fields triggers dependency.
func TestGenerate_GetAppWithStats(t *testing.T) {
	config := &GenerateConfig{
		SchemaVersion: "1",
		Package:       "main",
		Methods: map[string]*MethodConfig{
			"GetApp": {
				Fields:    []string{"Guid", "RunningInstances"},
				SubFields: make(map[string][]string),
			},
		},
	}

	output, err := Generate(config)
	if err != nil {
		t.Fatal(err)
	}

	src := string(output)

	// Stats depends on Process — both should appear.
	if !strings.Contains(src, "Processes.ListForApp") {
		t.Error("expected Processes.ListForApp (Process group force-activated)")
	}
	if !strings.Contains(src, "Processes.GetStats") {
		t.Error("expected Processes.GetStats (Stats group)")
	}

	// Should import "time" for stats Since field.
	if !strings.Contains(src, `"time"`) {
		t.Error("expected time import for stats")
	}

	assertCompiles(t, src)
}

// TestGenerate_GetAppWithRoutes verifies GetApp with Routes group.
func TestGenerate_GetAppWithRoutes(t *testing.T) {
	config := &GenerateConfig{
		SchemaVersion: "1",
		Package:       "main",
		Methods: map[string]*MethodConfig{
			"GetApp": {
				Fields:    []string{"Guid", "Routes"},
				SubFields: make(map[string][]string),
			},
		},
	}

	output, err := Generate(config)
	if err != nil {
		t.Fatal(err)
	}

	src := string(output)

	// Should have Routes group.
	if !strings.Contains(src, "Routes.ListForAppAll") {
		t.Error("expected Routes.ListForAppAll call")
	}

	// Should import "strings" for domain name extraction.
	if !strings.Contains(src, `"strings"`) {
		t.Error("expected strings import for route domain parsing")
	}

	assertCompiles(t, src)
}

// TestGenerate_GetAppFull verifies GetApp with all groups active.
func TestGenerate_GetAppFull(t *testing.T) {
	config := &GenerateConfig{
		SchemaVersion: "1",
		Package:       "main",
		Methods: map[string]*MethodConfig{
			"GetApp": {
				Fields: []string{
					"Guid", "Name", "State", "SpaceGuid",
					"Command", "Memory", "DiskQuota", "InstanceCount", "HealthCheckTimeout",
					"RunningInstances", "Instances",
					"BuildpackUrl", "PackageState", "StagingFailedReason",
					"Stack",
					"PackageUpdatedAt",
					"EnvironmentVars",
					"Routes",
					"Services",
				},
				SubFields: make(map[string][]string),
			},
		},
	}

	output, err := Generate(config)
	if err != nil {
		t.Fatal(err)
	}

	src := string(output)

	// All 9 groups should produce API calls.
	expectedCalls := []string{
		"Applications.Single",
		"Processes.ListForApp",
		"Processes.GetStats",
		"Droplets.GetCurrentForApp",
		"Stacks.Single",
		"Packages.ListForAppAll",
		"Applications.GetEnvironmentVariables",
		"Routes.ListForAppAll",
		"ServiceCredentialBindings.ListIncludeServiceInstancesAll",
	}
	for _, call := range expectedCalls {
		if !strings.Contains(src, call) {
			t.Errorf("expected %s call in generated code", call)
		}
	}

	// Header should say 9 calls.
	if !strings.Contains(src, "9 calls") {
		t.Error("expected '9 calls' in API call summary")
	}

	assertCompiles(t, src)
}

// TestGenerate_GetAppsBasic verifies the GetApps template.
func TestGenerate_GetAppsBasic(t *testing.T) {
	config := &GenerateConfig{
		SchemaVersion: "1",
		Package:       "main",
		Methods: map[string]*MethodConfig{
			"GetApps": {
				Fields:    []string{"Guid", "Name", "State"},
				SubFields: make(map[string][]string),
			},
		},
	}

	output, err := Generate(config)
	if err != nil {
		t.Fatal(err)
	}

	src := string(output)

	if !strings.Contains(src, "c.getApps()") {
		t.Error("expected GetApps dispatch")
	}
	if !strings.Contains(src, "Applications.ListAll") {
		t.Error("expected Applications.ListAll call")
	}

	// Should NOT have per-app process calls.
	if strings.Contains(src, "Processes.ListForApp") {
		t.Error("unexpected Process group when only basic fields requested")
	}

	assertCompiles(t, src)
}

// TestGenerate_GetAppsWithProcess verifies GetApps with per-app process fields.
func TestGenerate_GetAppsWithProcess(t *testing.T) {
	config := &GenerateConfig{
		SchemaVersion: "1",
		Package:       "main",
		Methods: map[string]*MethodConfig{
			"GetApps": {
				Fields:    []string{"Guid", "Name", "TotalInstances", "RunningInstances"},
				SubFields: make(map[string][]string),
			},
		},
	}

	output, err := Generate(config)
	if err != nil {
		t.Fatal(err)
	}

	src := string(output)

	// Stats depends on Process — both should appear.
	if !strings.Contains(src, "Processes.ListForApp") {
		t.Error("expected per-app Processes.ListForApp")
	}
	if !strings.Contains(src, "Processes.GetStats") {
		t.Error("expected per-app Processes.GetStats")
	}

	assertCompiles(t, src)
}

// TestGenerate_AllTenMethods verifies generating all 10 domain methods together.
func TestGenerate_AllTenMethods(t *testing.T) {
	config := &GenerateConfig{
		SchemaVersion: "1",
		Package:       "main",
		Methods: map[string]*MethodConfig{
			"GetApp":        {Fields: []string{"Guid", "Name"}, SubFields: make(map[string][]string)},
			"GetApps":       {Fields: []string{"Guid", "Name"}, SubFields: make(map[string][]string)},
			"GetOrg":        {Fields: []string{"Guid", "Name"}, SubFields: make(map[string][]string)},
			"GetOrgs":       {Fields: []string{"Guid", "Name"}, SubFields: make(map[string][]string)},
			"GetSpace":      {Fields: []string{"Guid", "Name"}, SubFields: make(map[string][]string)},
			"GetSpaces":     {Fields: []string{"Guid", "Name"}, SubFields: make(map[string][]string)},
			"GetService":    {Fields: []string{"Guid", "Name"}, SubFields: make(map[string][]string)},
			"GetServices":   {Fields: []string{"Guid", "Name"}, SubFields: make(map[string][]string)},
			"GetOrgUsers":   {Fields: []string{"Guid", "Username"}, SubFields: make(map[string][]string)},
			"GetSpaceUsers": {Fields: []string{"Guid", "Username"}, SubFields: make(map[string][]string)},
		},
	}

	output, err := Generate(config)
	if err != nil {
		t.Fatal(err)
	}

	src := string(output)

	// All 10 methods should dispatch to internal methods, not pass-through.
	dispatches := []string{
		"c.getApp(appName)", "c.getApps()",
		"c.getOrg(orgName)", "c.getOrgs()",
		"c.getSpace(spaceName)", "c.getSpaces()",
		"c.getService(serviceInstance)", "c.getServices()",
		"c.getOrgUsers(orgName, args...)", "c.getSpaceUsers(orgName, spaceName)",
	}
	for _, d := range dispatches {
		if !strings.Contains(src, d) {
			t.Errorf("expected dispatch %q in generated code", d)
		}
	}

	// No method should pass through to c.conn since all are configured.
	passThrough := []string{
		"c.conn.GetApp(", "c.conn.GetApps()",
		"c.conn.GetOrg(", "c.conn.GetOrgs()",
		"c.conn.GetSpace(", "c.conn.GetSpaces()",
		"c.conn.GetService(", "c.conn.GetServices()",
		"c.conn.GetOrgUsers(", "c.conn.GetSpaceUsers(",
	}
	for _, p := range passThrough {
		if strings.Contains(src, p) {
			t.Errorf("unexpected pass-through %q when all methods are configured", p)
		}
	}

	assertCompiles(t, src)
}

// assertCompiles verifies the generated Go source parses without errors.
func assertCompiles(t *testing.T, src string) {
	t.Helper()
	fset := token.NewFileSet()
	_, err := parser.ParseFile(fset, "generated.go", src, parser.AllErrors)
	if err != nil {
		t.Errorf("generated code does not parse: %v\n\nSource:\n%s", err, src)
	}
}
