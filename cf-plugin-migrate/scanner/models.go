package scanner

// ModelInfo describes the fields of a V2 plugin model and how they map to
// V3 API call groups. The scanner uses this to categorize detected field
// access and the YAML output uses it to annotate API call costs.
type ModelInfo struct {
	// Groups lists the dependency groups in execution order.
	// Each group represents one or more V3 API calls.
	Groups []Group

	// FieldGroup maps top-level field name to its group index.
	FieldGroup map[string]int

	// SubFieldKeys maps composite field name to its YAML sub-field key.
	// e.g., "Routes" → "route_fields"
	SubFieldKeys map[string]string
}

// Group represents a set of fields that share the same V3 API call(s).
type Group struct {
	Name    string // e.g., "App", "Process", "Routes"
	APICall string // e.g., "Applications.Single", "Processes.ListForApp + Processes.Get"
	PerItem bool   // true if this group requires per-item calls (e.g., GetApps process fields)
}

// V2Methods is the set of V2 domain method names the scanner looks for.
var V2Methods = map[string]bool{
	"GetApp":        true,
	"GetApps":       true,
	"GetService":    true,
	"GetServices":   true,
	"GetOrg":        true,
	"GetOrgs":       true,
	"GetSpace":      true,
	"GetSpaces":     true,
	"GetOrgUsers":   true,
	"GetSpaceUsers": true,
}

// V2Models maps V2 method names to their model field structure.
var V2Models = map[string]*ModelInfo{
	"GetApp": {
		Groups: []Group{
			{Name: "App", APICall: "Applications.Single"},
			{Name: "Process", APICall: "Processes.ListForApp + Processes.Get"},
			{Name: "Stats", APICall: "Processes.GetStats"},
			{Name: "Droplet", APICall: "Droplets.GetCurrentForApp"},
			{Name: "Stack", APICall: "Stacks.Single"},
			{Name: "Package", APICall: "Packages.ListForAppAll"},
			{Name: "Env", APICall: "Applications.GetEnvironmentVariables"},
			{Name: "Routes", APICall: "Routes.ListForApp(include=domain)"},
			{Name: "Services", APICall: "ServiceCredentialBindings.List(include=service_instance)"},
		},
		FieldGroup: map[string]int{
			"Guid":                 0, // App
			"Name":                 0,
			"State":                0,
			"SpaceGuid":            0,
			"Command":              1, // Process
			"DetectedStartCommand": 1,
			"DiskQuota":            1,
			"InstanceCount":        1,
			"Memory":               1,
			"HealthCheckTimeout":   1,
			"RunningInstances":     2, // Stats
			"Instances":            2,
			"BuildpackUrl":         3, // Droplet
			"PackageState":         3,
			"StagingFailedReason":  3,
			"Stack":                4, // Stack
			"PackageUpdatedAt":     5, // Package
			"EnvironmentVars":      6, // Env
			"Routes":               7, // Routes
			"Services":             8, // Services
		},
		SubFieldKeys: map[string]string{
			"Stack":     "stack_fields",
			"Instances": "instance_fields",
			"Routes":    "route_fields",
			"Services":  "service_fields",
		},
	},
	"GetApps": {
		Groups: []Group{
			{Name: "Apps", APICall: "Applications.ListAll"},
			{Name: "Process", APICall: "Processes.ListForApp", PerItem: true},
			{Name: "Stats", APICall: "Processes.GetStats", PerItem: true},
			{Name: "Routes", APICall: "Routes.ListForApp(include=domain)", PerItem: true},
		},
		FieldGroup: map[string]int{
			"Guid":             0, // Apps
			"Name":             0,
			"State":            0,
			"TotalInstances":   1, // Process (per-app)
			"Memory":           1,
			"DiskQuota":        1,
			"RunningInstances": 2, // Stats (per-app)
			"Routes":           3, // Routes (per-app)
		},
		SubFieldKeys: map[string]string{
			"Routes": "route_fields",
		},
	},
	"GetService": {
		Groups: []Group{
			{Name: "Instance+Plan+Offering", APICall: "ServiceInstances.Single(fields[service_plan], fields[service_plan.service_offering])"},
		},
		FieldGroup: map[string]int{
			"Guid":            0,
			"Name":            0,
			"DashboardUrl":    0,
			"IsUserProvided":  0,
			"LastOperation":   0,
			"ServicePlan":     0,
			"ServiceOffering": 0,
		},
		SubFieldKeys: map[string]string{
			"LastOperation":   "last_operation_fields",
			"ServicePlan":     "service_plan_fields",
			"ServiceOffering": "service_offering_fields",
		},
	},
	"GetServices": {
		Groups: []Group{
			{Name: "Instances+Plans+Offerings", APICall: "ServiceInstances.ListAll(fields[service_plan], fields[service_plan.service_offering])"},
			{Name: "Apps", APICall: "ServiceCredentialBindings.ListAll(include=app)"},
		},
		FieldGroup: map[string]int{
			"Guid":             0,
			"Name":             0,
			"IsUserProvided":   0,
			"LastOperation":    0,
			"ServicePlan":      0,
			"Service":          0,
			"ApplicationNames": 1,
		},
		SubFieldKeys: map[string]string{
			"LastOperation": "last_operation_fields",
			"ServicePlan":   "service_plan_fields",
			"Service":       "service_fields",
		},
	},
	"GetOrg": {
		Groups: []Group{
			{Name: "Org", APICall: "Organizations.Single"},
			{Name: "Quota", APICall: "OrganizationQuotas.Get"},
			{Name: "Spaces", APICall: "Spaces.ListAll"},
			{Name: "Domains", APICall: "Domains.ListForOrganization"},
			{Name: "SpaceQuotas", APICall: "SpaceQuotas.ListAll"},
		},
		FieldGroup: map[string]int{
			"Guid":            0,
			"Name":            0,
			"QuotaDefinition": 1,
			"Spaces":          2,
			"Domains":         3,
			"SpaceQuotas":     4,
		},
		SubFieldKeys: map[string]string{
			"QuotaDefinition": "quota_fields",
			"Spaces":          "space_fields",
			"Domains":         "domain_fields",
			"SpaceQuotas":     "space_quota_fields",
		},
	},
	"GetOrgs": {
		Groups: []Group{
			{Name: "Orgs", APICall: "Organizations.ListAll"},
		},
		FieldGroup: map[string]int{
			"Guid": 0,
			"Name": 0,
		},
		SubFieldKeys: map[string]string{},
	},
	"GetSpace": {
		Groups: []Group{
			{Name: "Space+Org", APICall: "Spaces.Single(include=organization)"},
			{Name: "Apps", APICall: "Applications.ListAll"},
			{Name: "Services", APICall: "ServiceInstances.ListAll"},
			{Name: "Domains", APICall: "Domains.ListAll"},
			{Name: "SecurityGroups", APICall: "SecurityGroups.ListAll"},
			{Name: "SpaceQuota", APICall: "SpaceQuotas.Get"},
		},
		FieldGroup: map[string]int{
			"Guid":             0,
			"Name":             0,
			"Organization":     0,
			"Applications":     1,
			"ServiceInstances": 2,
			"Domains":          3,
			"SecurityGroups":   4,
			"SpaceQuota":       5,
		},
		SubFieldKeys: map[string]string{
			"Organization":     "org_fields",
			"Applications":     "app_fields",
			"ServiceInstances": "service_instance_fields",
			"Domains":          "domain_fields",
			"SecurityGroups":   "security_group_fields",
			"SpaceQuota":       "space_quota_fields",
		},
	},
	"GetSpaces": {
		Groups: []Group{
			{Name: "Spaces", APICall: "Spaces.ListAll"},
		},
		FieldGroup: map[string]int{
			"Guid": 0,
			"Name": 0,
		},
		SubFieldKeys: map[string]string{},
	},
	"GetOrgUsers": {
		Groups: []Group{
			{Name: "Roles+Users", APICall: "Organizations.Single + Roles.ListAll(include=user)"},
		},
		FieldGroup: map[string]int{
			"Guid":     0,
			"Username": 0,
			"IsAdmin":  0,
			"Roles":    0,
		},
		SubFieldKeys: map[string]string{},
	},
	"GetSpaceUsers": {
		Groups: []Group{
			{Name: "Roles+Users", APICall: "Organizations.Single + Spaces.Single + Roles.ListAll(include=user)"},
		},
		FieldGroup: map[string]int{
			"Guid":     0,
			"Username": 0,
			"IsAdmin":  0,
			"Roles":    0,
		},
		SubFieldKeys: map[string]string{},
	},
}
