# Meta
[meta]: #meta
- Name: CLI Plugin Transitional Migration Guide
- Start Date: 2026-03-01
- Author(s): @norman-abramovitz
- Status: Draft
- Related RFC: [CLI Plugin Interface V3](rfc-draft-cli-plugin-interface-v3.md)
- Tracking Issue: [cloudfoundry/cli#3621](https://github.com/cloudfoundry/cli/issues/3621)

## Summary

This document describes a **guest-side transitional migration approach** that allows existing Go plugins to begin migrating from V2 domain methods to direct CAPI V3 access **today**, without waiting for the V3 plugin interface or any host (CF CLI) changes. The approach uses a companion wrapper library that sits entirely on the guest side, wraps the existing `plugin.CliConnection`, and provides a pre-configured go-cfclient V3 client.

This approach is validated by the [Rabobank `cf-plugins`](https://github.com/rabobank/cf-plugins) library, which has been in production use since 2025 (see [plugin survey case study](plugin-survey.md#case-study-rabobank-guest-side-transitional-wrapper)).

## Motivation

### Why Migrate Now?

1. **CAPI V2 is reaching end of life.** Plugins that depend on V2-shaped data from the host's domain methods (`GetApp`, `GetApps`, `GetService`, etc.) will stop working when V2 endpoints are removed.

2. **V2 domain methods are lossy.** The V2-shaped `plugin_models.*` types cannot represent V3 concepts: multiple process types, sidecars, rolling deployments, service credential bindings, metadata labels, etc. Plugins reimplementing V2 methods via V3 (as Rabobank demonstrated) lose information and require many more API calls.

3. **The V3 plugin interface is not yet available.** The [Plugin Interface V3 RFC](rfc-draft-cli-plugin-interface-v3.md) defines a new interface with embedded metadata, JSON-RPC, and polyglot support — but implementation requires host changes across multiple phases (Q3 2026 – Q3 2027). Plugin developers should not wait.

4. **No host changes required.** A guest-side wrapper works with any existing CF CLI version (v7, v8, v9) because it only uses the standard `plugin.CliConnection` interface and `plugin.Start()` entry point.

### Who Should Use This Approach?

- Plugin developers whose plugins call V2 domain methods (`GetApp`, `GetApps`, `GetService`, `GetServices`, `GetOrg`, `GetOrgs`, `GetSpaces`)
- Plugin developers who want to access CAPI V3 directly but currently bootstrap go-cfclient or custom HTTP clients manually
- Plugin developers who want to prepare for the V3 interface by adopting the companion package pattern now

## Architecture

### Terminology

Per the [V3 RFC terminology](rfc-draft-cli-plugin-interface-v3.md#terminology): **Host** is the CF CLI process, **Guest** is the plugin process.

### Design

The transitional approach introduces a thin wrapper library on the guest side:

```
┌─────────────────────────────────────────────────────┐
│                  Host (CF CLI)                       │
│                                                     │
│  Existing plugin.CliConnection via net/rpc + gob    │
│  (no changes required)                              │
└────────────────────────┬────────────────────────────┘
                         │
                         │  Existing gob/net-rpc protocol
                         │
┌────────────────────────▼────────────────────────────┐
│                  Guest (Plugin)                      │
│                                                     │
│  ┌───────────────────────────────────────────────┐  │
│  │  Transitional Wrapper                         │  │
│  │                                               │  │
│  │  ┌─────────────────┐  ┌───────────────────┐  │  │
│  │  │ Pass-through     │  │ go-cfclient V3    │  │  │
│  │  │ (context methods │  │ (constructed from │  │  │
│  │  │  delegate to     │  │  AccessToken +    │  │  │
│  │  │  original conn)  │  │  ApiEndpoint)     │  │  │
│  │  └─────────────────┘  └───────────────────┘  │  │
│  └───────────────────────────────────────────────┘  │
│                                                     │
│  Plugin domain logic uses go-cfclient V3 directly   │
└─────────────────────────────────────────────────────┘
```

The wrapper:
- **Embeds** `plugin.CliConnection` — all 16 context methods pass through unchanged
- **Constructs** a go-cfclient V3 `*client.Client` from `AccessToken()`, `ApiEndpoint()`, and `IsSSLDisabled()`
- **Exposes** `CfClient()` so the plugin can make direct V3 API calls
- **Calls** `plugin.Start()` internally — the standard host registration mechanism

## Migration Guide

### Step 1: Add the Companion Package

The recommended companion package is `code.cloudfoundry.org/cli-plugin-helpers/cfclient` (proposed in the V3 RFC). Until that package is published, plugin developers can use the pattern directly.

Add go-cfclient to your plugin's dependencies:

```bash
go get github.com/cloudfoundry/go-cfclient/v3
```

### Step 2: Create a Client Helper

Add a helper function that constructs a go-cfclient V3 client from the existing plugin connection:

```go
package main

import (
    "code.cloudfoundry.org/cli/plugin"
    "github.com/cloudfoundry/go-cfclient/v3/client"
    "github.com/cloudfoundry/go-cfclient/v3/config"
)

func newCfClient(conn plugin.CliConnection) (*client.Client, error) {
    endpoint, err := conn.ApiEndpoint()
    if err != nil {
        return nil, err
    }

    token, err := conn.AccessToken()
    if err != nil {
        return nil, err
    }

    skipSSL, err := conn.IsSSLDisabled()
    if err != nil {
        return nil, err
    }

    cfg, err := config.New(endpoint,
        config.Token(token),
        config.SkipTLSValidation(skipSSL),
    )
    if err != nil {
        return nil, err
    }

    return client.New(cfg)
}
```

**Note:** The `config.Token()` function in go-cfclient handles the `"bearer "` prefix internally. Do not strip the prefix manually.

### Step 3: Replace V2 Domain Method Calls with Direct V3 Access

**Before — using V2 domain method from the host:**

```go
func (p *MyPlugin) Run(conn plugin.CliConnection, args []string) {
    // V2-shaped response from host — limited fields, single process model
    app, err := conn.GetApp("my-app")
    if err != nil {
        fmt.Println(err)
        return
    }
    fmt.Printf("App: %s (%s)\n", app.Name, app.Guid)
    fmt.Printf("State: %s, Instances: %d\n", app.State, app.InstanceCount)
}
```

**After — using go-cfclient V3 directly:**

```go
func (p *MyPlugin) Run(conn plugin.CliConnection, args []string) {
    cfClient, err := newCfClient(conn)
    if err != nil {
        fmt.Println(err)
        return
    }

    space, err := conn.GetCurrentSpace()
    if err != nil {
        fmt.Println(err)
        return
    }

    // V3-native response — full app model with metadata, lifecycle, relationships
    app, err := cfClient.Applications.Single(context.Background(),
        &client.AppListOptions{
            Names:      client.Filter{Values: []string{"my-app"}},
            SpaceGUIDs: client.Filter{Values: []string{space.Guid}},
        },
    )
    if err != nil {
        fmt.Println(err)
        return
    }
    fmt.Printf("App: %s (%s)\n", app.Name, app.GUID)
    fmt.Printf("State: %s\n", app.State)

    // V3 gives access to processes, sidecars, metadata — not available via V2
    processes, _, err := cfClient.Processes.ListForApp(context.Background(), app.GUID, nil)
    if err == nil {
        for _, proc := range processes {
            fmt.Printf("  Process: %s, Instances: %d, Memory: %dMB\n",
                proc.Type, proc.Instances, proc.MemoryInMB)
        }
    }
}
```

### Step 4: Remove V2 Domain Method Usage Incrementally

Migration does not need to happen all at once. A plugin can replace one V2 method call at a time:

| V2 Method | V3 Replacement (go-cfclient) |
|---|---|
| `conn.GetApp(name)` | `cfClient.Applications.Single(ctx, &client.AppListOptions{Names: ..., SpaceGUIDs: ...})` |
| `conn.GetApps()` | `cfClient.Applications.ListAll(ctx, &client.AppListOptions{SpaceGUIDs: ...})` |
| `conn.GetService(name)` | `cfClient.ServiceInstances.Single(ctx, &client.ServiceInstanceListOptions{Names: ..., SpaceGUIDs: ...})` |
| `conn.GetServices()` | `cfClient.ServiceInstances.ListAll(ctx, &client.ServiceInstanceListOptions{SpaceGUIDs: ...})` |
| `conn.GetOrg(name)` | `cfClient.Organizations.Single(ctx, &client.OrganizationListOptions{Names: ...})` |
| `conn.GetOrgs()` | `cfClient.Organizations.ListAll(ctx, nil)` |
| `conn.GetSpaces()` | `cfClient.Spaces.ListAll(ctx, &client.SpaceListOptions{OrganizationGUIDs: ...})` |

Context methods (`AccessToken`, `ApiEndpoint`, `GetCurrentOrg`, `GetCurrentSpace`, `Username`, `IsLoggedIn`, `HasOrganization`, `HasSpace`, `IsSSLDisabled`, `HasAPIEndpoint`, `ApiVersion`) continue to use the existing `plugin.CliConnection` — these are the core contract methods that the V3 RFC preserves.

### Alternative: Generated V2 Compatibility Wrappers

For plugins with extensive V2 domain method usage, rewriting every call site at once may not be practical. An alternative approach generates **minimal V2-compatible wrapper functions** that return the existing `plugin_models.*` types but only populate the fields the plugin actually uses, backed by the minimum V3 API calls required.

#### How It Works

The plugin developer declares which V2 methods they call and which fields they use:

```yaml
# cf-plugin-migrate.yml
methods:
  GetApp:
    fields: [Name, Guid, State, Routes]
  GetApps:
    fields: [Name, Guid, State]
  GetService:
    fields: [Name, Guid, ServicePlan]
```

A code generator reads this configuration and produces tailored wrapper functions:

```go
// GENERATED by cf-plugin-migrate — do not edit
// GetApp populates: Name, Guid, State, Routes
// V3 API calls: Applications.Single, Routes.ListForApp (2 calls)
func getApp(cfClient *client.Client, spaceGUID string, name string) (plugin_models.GetAppModel, error) {
    var model plugin_models.GetAppModel

    app, err := cfClient.Applications.Single(context.Background(),
        &client.AppListOptions{
            Names:      client.Filter{Values: []string{name}},
            SpaceGUIDs: client.Filter{Values: []string{spaceGUID}},
        })
    if err != nil {
        return model, err
    }

    model.Name = app.Name
    model.Guid = app.GUID
    model.State = string(app.State)

    // Routes — requested by developer
    routes, _, err := cfClient.Routes.ListForApp(context.Background(), app.GUID, nil)
    if err == nil {
        for _, r := range routes {
            model.Routes = append(model.Routes, plugin_models.GetApp_RouteSummary{
                Guid: r.GUID,
                Host: r.Host,
            })
        }
    }

    return model, nil
}
```

Fields not listed in the configuration remain zero-valued. The generated code documents exactly which V3 API calls it makes, making the cost transparent.

#### Field-to-API-Call Mapping

The generator uses a field dependency map to determine the minimum V3 calls needed:

| V2 Fields Requested | V3 API Calls Required | Call Count |
|---|---|---|
| `Name`, `Guid`, `State`, `Memory`, `DiskQuota` | `Applications.Single()` | 1 |
| + `Routes` | + `Routes.ListForApp()` | 2 |
| + `RunningInstances`, `Instances` | + `Processes.ListForApp()` + `ProcessStats` | 3–4 |
| + `BuildpackUrl` | + `Droplets.ListForApp()` | 3–5 |
| + `EnvironmentVars` | + `Applications.GetEnvironment()` | 4–6 |
| + `Services` | + `ServiceCredentialBindings.ListAll()` | 5–7 |

Compare this to Rabobank's approach which always makes 11 calls to populate every field, regardless of what the plugin uses.

#### Migration Path with Generated Wrappers

1. **Audit** — List the V2 domain methods your plugin calls and which fields it reads from the returned structs
2. **Configure** — Write the YAML configuration declaring methods and fields
3. **Generate** — Run `cf-plugin-migrate generate` to produce the wrapper functions
4. **Replace** — Change `conn.GetApp(name)` to `getApp(cfClient, space.Guid, name)` — same return type, existing code compiles unchanged
5. **Evolve** — When ready, switch call sites to use go-cfclient V3 types directly and delete the generated wrappers

This approach is a stepping stone, not a destination. The generated wrappers let plugins migrate incrementally without rewriting all domain logic at once, while avoiding the full cost of Rabobank's complete V2 reimplementation.

#### Worked Example: OCF Scheduler Plugin

The [OCF Scheduler plugin](https://github.com/cloudfoundry-community/ocf-scheduler-cf-plugin) demonstrates the generator approach on a real, actively maintained plugin. Source analysis reveals minimal V2 field usage:

**Audit results:**

| V2 Method | Call Sites | Fields Accessed | Purpose |
|---|---|---|---|
| `GetApp(name)` | `commands/create-job.go`, `commands/create-call.go` | `.Guid` only | Name→GUID resolution for Scheduler API calls |
| `GetApps()` | `core/util.go:MyApps()` → 6 command files via `AppByGUID()` | `.Guid` (matching), `.Name` (display) | GUID→Name resolution for table output |

No other fields are accessed — not `State`, `Routes`, `Memory`, `Instances`, or any other model attribute. The plugin uses the V2 model methods purely as a name/GUID mapping layer.

**Configuration — `cf-plugin-migrate.yml`:**

```yaml
# ocf-scheduler-cf-plugin/cf-plugin-migrate.yml
package: main
methods:
  GetApp:
    fields: [Name, Guid]
  GetApps:
    fields: [Name, Guid]
```

**Generated code — `v2compat_generated.go`:**

```go
// GENERATED by cf-plugin-migrate — do not edit
//
// Source: cf-plugin-migrate.yml
// V3 API calls: Applications.Single (1 call per GetApp),
//               Applications.ListAll (1 call per GetApps)

package main

import (
	"context"

	"code.cloudfoundry.org/cli/plugin/models"
	"github.com/cloudfoundry/go-cfclient/v3/client"
)

// getApp resolves an app name to a GetAppModel.
// Populates: Name, Guid (1 V3 API call).
func getApp(cfClient *client.Client, spaceGUID string, name string) (plugin_models.GetAppModel, error) {
	var model plugin_models.GetAppModel

	app, err := cfClient.Applications.Single(context.Background(),
		&client.AppListOptions{
			Names:      client.Filter{Values: []string{name}},
			SpaceGUIDs: client.Filter{Values: []string{spaceGUID}},
		})
	if err != nil {
		return model, err
	}

	model.Name = app.Name
	model.Guid = app.GUID
	return model, nil
}

// getApps lists all apps in a space.
// Populates: Name, Guid (1 V3 API call).
func getApps(cfClient *client.Client, spaceGUID string) ([]plugin_models.GetAppsModel, error) {
	apps, err := cfClient.Applications.ListAll(context.Background(),
		&client.AppListOptions{
			SpaceGUIDs: client.Filter{Values: []string{spaceGUID}},
		})
	if err != nil {
		return nil, err
	}

	result := make([]plugin_models.GetAppsModel, len(apps))
	for i, app := range apps {
		result[i].Name = app.Name
		result[i].Guid = app.GUID
	}
	return result, nil
}
```

**Call site changes — 3 lines per site:**

`commands/create-job.go` (and identically `create-call.go`):
```go
// Before:
app, err := services.CLI.GetApp(appName)

// After:
app, err := getApp(services.CfClient, services.SpaceGUID, appName)
```

`core/util.go`:
```go
// Before:
func MyApps(services *Services) ([]models.GetAppsModel, error) {
    return services.CLI.GetApps()
}

// After:
func MyApps(services *Services) ([]models.GetAppsModel, error) {
    return getApps(services.CfClient, services.SpaceGUID)
}
```

**What changes in `core/services.go`:**

The `Services` struct gains a `CfClient` and `SpaceGUID`, initialized once at startup:

```go
// Before:
type Services struct {
    CLI    plugin.CliConnection
    Client core.Driver
    UI     *ui.UI
}

// After:
type Services struct {
    CLI       plugin.CliConnection
    Client    core.Driver
    UI        *ui.UI
    CfClient  *client.Client   // go-cfclient V3
    SpaceGUID string           // cached from GetCurrentSpace()
}
```

**Result:**

| Metric | Before (V2) | After (Generated) | Rabobank equivalent |
|---|---|---|---|
| V3 API calls for `GetApp()` | 0 (host handles it) | **1** | 11 |
| V3 API calls for `GetApps()` | 0 (host handles it) | **1** | 11 × N |
| Fields populated | All (unused) | **2** (Name, Guid) | All (unused) |
| New dependencies | None | go-cfclient/v3 | go-cfclient/v3 |
| Lines of generated code | N/A | **~40** | ~500 (full reimplementation) |
| Plugin code changes | N/A | **~10 lines** | ~10 lines |

The `AppByGUID` helper in `core/util.go` requires no changes — it already operates on `models.GetAppsModel` which the generated `getApps()` returns. All 6 command files that call `MyApps()` → `AppByGUID()` continue to work unchanged.

#### Worked Example: metric-registrar Plugin (Complex Migration)

The [metric-registrar plugin](https://github.com/pivotal-cf/metric-registrar-cli) is the most V2-coupled active plugin in the survey. It uses three V2 domain methods **and** four V2 CAPI curl calls. This analysis covers the migration in two phases: helper functions first, then curl calls.

##### Phase 1: Helper Functions (V2 Domain Methods)

**Audit results:**

| V2 Method | Call Sites | Fields Accessed | Purpose |
|---|---|---|---|
| `GetApp(name)` | `register.go` (1), `unregister.go` (2) | `.Guid`, `.Name`, `.Routes[].Host`, `.Routes[].Domain.Name`, `.Routes[].Port`, `.Routes[].Path` | GUID for API calls; Name for errors; Routes for endpoint validation |
| `GetApps()` | `list.go` (2 — log formats + metrics endpoints) | `.Guid`, `.Name` | GUID→Name mapping for display |
| `GetServices()` | `register.go` (1) | `.Name` only | Check if UPS already exists by name |
| `GetCurrentSpace()` | `registrations/fetcher.go` (1) | `.Guid` only | Space GUID for V2 UPS listing |

**Complexity difference from OCF Scheduler:** `GetApp()` here accesses **Routes with sub-fields** — this is the first case that needs a second V3 API call beyond `Applications.Single()`.

**Configuration — `cf-plugin-migrate.yml`:**

```yaml
# metric-registrar-cli/cf-plugin-migrate.yml
package: command
methods:
  GetApp:
    fields: [Name, Guid, Routes]
    route_fields: [Host, Domain.Name, Port, Path]
  GetApps:
    fields: [Name, Guid]
```

Note: `GetServices()` uses only `.Name` — the generator could produce a wrapper, but this is simple enough to replace directly with `cfClient.ServiceInstances.ListAll()`.

**Generated code — `v2compat_generated.go`:**

```go
// GENERATED by cf-plugin-migrate — do not edit
//
// Source: cf-plugin-migrate.yml
// V3 API calls: Applications.Single + Routes.ListForApp (2 calls per GetApp),
//               Applications.ListAll (1 call per GetApps)

package command

import (
	"context"

	"code.cloudfoundry.org/cli/plugin/models"
	"github.com/cloudfoundry/go-cfclient/v3/client"
)

// getApp resolves an app name to a GetAppModel.
// Populates: Name, Guid, Routes (2 V3 API calls).
func getApp(cfClient *client.Client, spaceGUID string, name string) (plugin_models.GetAppModel, error) {
	var model plugin_models.GetAppModel

	app, err := cfClient.Applications.Single(context.Background(),
		&client.AppListOptions{
			Names:      client.Filter{Values: []string{name}},
			SpaceGUIDs: client.Filter{Values: []string{spaceGUID}},
		})
	if err != nil {
		return model, err
	}

	model.Name = app.Name
	model.Guid = app.GUID

	// Routes — requested by developer (adds 1 API call)
	routes, _, err := cfClient.Routes.ListForApp(context.Background(), app.GUID, nil)
	if err != nil {
		return model, err
	}
	for _, r := range routes {
		route := plugin_models.GetApp_RouteSummary{
			Host: r.Host,
			Path: r.Path,
			Port: r.Port,
			Domain: plugin_models.GetApp_DomainFields{
				Name: r.DomainName(),
			},
		}
		model.Routes = append(model.Routes, route)
	}

	return model, nil
}

// getApps lists all apps in a space.
// Populates: Name, Guid (1 V3 API call).
func getApps(cfClient *client.Client, spaceGUID string) ([]plugin_models.GetAppsModel, error) {
	apps, err := cfClient.Applications.ListAll(context.Background(),
		&client.AppListOptions{
			SpaceGUIDs: client.Filter{Values: []string{spaceGUID}},
		})
	if err != nil {
		return nil, err
	}

	result := make([]plugin_models.GetAppsModel, len(apps))
	for i, app := range apps {
		result[i].Name = app.Name
		result[i].Guid = app.GUID
	}
	return result, nil
}
```

**`GetServices()` replacement — direct, no generator needed:**

```go
// Before (register.go):
existingServices, err := cliConn.GetServices()
for _, s := range existingServices {
    if s.Name == serviceName { ... }
}

// After:
instances, err := cfClient.ServiceInstances.ListAll(ctx,
    &client.ServiceInstanceListOptions{
        Names:      client.Filter{Values: []string{serviceName}},
        SpaceGUIDs: client.Filter{Values: []string{spaceGUID}},
        Type:       "user-provided",
    })
exists := len(instances) > 0
```

The V3 replacement is actually *better* — it filters server-side by name instead of fetching all services and scanning client-side.

**Phase 1 result:**

| Metric | Before (V2) | After (Generated) |
|---|---|---|
| V3 API calls for `GetApp()` | 0 (host handles it) | **2** (app + routes) |
| V3 API calls for `GetApps()` | 0 (host handles it) | **1** |
| V3 API calls for `GetServices()` | 0 (host handles it) | **1** (server-side filtered) |
| Fields populated in GetAppModel | All | **6** (Name, Guid, Routes.Host/Domain.Name/Port/Path) |
| New dependencies | None | go-cfclient/v3 |

Phase 1 can be completed independently. The plugin continues to use `CliCommandWithoutTerminalOutput("curl", ...)` for the V2 endpoints until Phase 2.

##### Phase 2: Replacing V2 `cf curl` Calls

The plugin makes four V2 curl calls. Two migrate cleanly; two require a structural redesign.

**V2→V3 endpoint mapping:**

| V2 Endpoint | V3 Equivalent | Migration Difficulty |
|---|---|---|
| GET `/v2/user_provided_service_instances?q=space_guid:{guid}` | GET `/v3/service_instances?type=user-provided&space_guids={guid}` | **Clean.** `syslog_drain_url` is a top-level field in V3. |
| GET `/v2/.../service_bindings` | GET `/v3/service_credential_bindings?service_instance_guids={guid}&type=app` | **Clean.** App GUID at `relationships.app.data.guid` instead of `entity.app_guid`. |
| GET `/v2/apps/{guid}` (read `.entity.ports`) | GET `/v3/apps/{guid}/routes` + GET `/v3/routes/{route-guid}/destinations` | **Structural change.** Ports moved from app entity to route destinations. |
| PUT `/v2/apps/{guid}` `{"ports":[...]}` | POST/PATCH `/v3/routes/{route-guid}/destinations` | **Structural change.** Must know route GUID, construct destination objects per route. |

**Clean migrations (registrations/fetcher.go):**

The fetcher currently pages through V2 UPS instances and their bindings:

```go
// Before — V2 curl:
path := fmt.Sprintf("/v2/user_provided_service_instances?q=space_guid:%s", space.Guid)
// Parses: resources[].entity.{name, syslog_drain_url, service_bindings_url}
// Then follows service_bindings_url to get: resources[].entity.app_guid

// After — go-cfclient V3:
instances, err := cfClient.ServiceInstances.ListAll(ctx,
    &client.ServiceInstanceListOptions{
        SpaceGUIDs: client.Filter{Values: []string{space.Guid}},
        Type:       "user-provided",
    })
// syslog_drain_url is instance.SyslogDrainURL (top-level field)

// For each instance, get bindings:
bindings, err := cfClient.ServiceCredentialBindings.ListAll(ctx,
    &client.ServiceCredentialBindingListOptions{
        ServiceInstanceGUIDs: client.Filter{Values: []string{instance.GUID}},
        Type:                 "app",
    })
// app GUID is binding.Relationships.App.Data.GUID
```

This replaces the manual `getPagedResource()` pagination, JSON parsing, and URL-following with typed SDK calls. The `paginatedResp`, `servicesResponse`, `serviceEntity`, `bindingsResponse`, and `bindingEntity` types can all be deleted.

**Structural change (ports/ports.go):**

The V2 app entity had a flat `ports` array. In V3, ports are a property of **route destinations** — the mapping between a route and an app process.

```go
// Before — V2: read ports from app entity
// GET /v2/apps/{guid} → .entity.ports → [8080, 9090]

// After — V3: read ports from route destinations
routes, _, err := cfClient.Routes.ListForApp(ctx, appGUID, nil)
var ports []int
for _, route := range routes {
    dests, _, err := cfClient.RouteDestinations.ListForRoute(ctx, route.GUID, nil)
    for _, dest := range dests {
        if dest.App.GUID == appGUID {
            ports = append(ports, dest.Port)
        }
    }
}
```

```go
// Before — V2: set ports on app entity
// PUT /v2/apps/{guid} {"ports": [8080, 9090]}

// After — V3: add destination to route
// Must first identify the correct route, then add a destination with the desired port
_, err := cfClient.RouteDestinations.Add(ctx, routeGUID, client.RouteDestinationAddOptions{
    Destinations: []client.RouteDestination{
        {
            App:      client.RouteDestinationApp{GUID: appGUID, Process: &client.Process{Type: "web"}},
            Port:     9090,
            Protocol: "http1",
        },
    },
})
```

**The port migration is a redesign, not a substitution.** The plugin currently thinks of ports as a property of the app. In V3, ports are a property of the route-to-app mapping. The plugin's `ports/ports.go` module needs:

1. A way to discover the app's routes (which it already has from `GetApp().Routes` after Phase 1)
2. Logic to map between the V2 flat-ports model and V3 per-route destinations
3. Handling for apps with multiple routes (each route has independent destinations)

This is the kind of migration that a generated wrapper **cannot solve** — it requires understanding the V3 domain model. The recommended approach is to rewrite `ports/ports.go` using go-cfclient's `Routes` and `RouteDestinations` resources directly.

> **TODO:** The V2 ports → V3 route destinations migration deserves deeper analysis. Key open questions: How do route destinations interact with internal routes (used by metric-registrar for metrics endpoint exposure)? Does the `cf create-route --internal` + destination model fully replace the V2 ports array for internal service-to-service communication? Are there other plugins or CF features that depend on the V2 ports model? This analysis should be completed before the metric-registrar migration guide is finalized.

**CLI command delegation (unchanged in both phases):**

The four delegated CLI commands (`create-user-provided-service`, `bind-service`, `unbind-service`, `delete-service`) continue to use `CliCommandWithoutTerminalOutput`. These are multi-step workflow operations that the CLI manages internally. They can be replaced with go-cfclient calls as a future optimization, but they work correctly as-is and do not depend on V2 endpoints.

| CLI Command | Can migrate to go-cfclient? | Priority |
|---|---|---|
| `create-user-provided-service` | Yes: `cfClient.ServiceInstances.CreateUserProvided()` | Low — CLI handles it fine |
| `bind-service` | Yes: `cfClient.ServiceCredentialBindings.Create()` | Low |
| `unbind-service` | Yes: `cfClient.ServiceCredentialBindings.Delete()` | Low |
| `delete-service` | Yes: `cfClient.ServiceInstances.Delete()` | Low |

**Phase 2 result:**

| Component | Migration Type | Effort |
|---|---|---|
| `registrations/fetcher.go` — UPS listing | Clean V3 substitution | Low — swap URL patterns and JSON paths |
| `registrations/fetcher.go` — binding lookup | Clean V3 substitution | Low — `relationships.app.data.guid` |
| `ports/ports.go` — read ports | Structural redesign | **High** — flat array → per-route destinations |
| `ports/ports.go` — write ports | Structural redesign | **High** — app-centric → route-centric |
| CLI command delegation | Optional future work | Low — works as-is |

##### Key Insight

The metric-registrar migration reveals two distinct categories of V2→V3 work:

1. **Substitution** — Same concept, different API shape. Domain methods (`GetApp`, `GetApps`, `GetServices`) and flat V2 endpoints (UPS listing, binding lookup) map to V3 equivalents with minor field-path changes. The generated wrapper approach handles this well.

2. **Redesign** — The V3 model is fundamentally different. App ports moving to route destinations is not a field rename — it's a different domain model. No generator can bridge this; the plugin developer must understand the V3 model and rewrite the affected code.

The transitional approach handles category 1 automatically and surfaces category 2 as the work that requires human judgment.

### Step 5: Remove `CliCommand` / `CliCommandWithoutTerminalOutput` Usage

Plugins that use `CliCommandWithoutTerminalOutput("curl", "/v3/...")` for CAPI access SHOULD migrate to go-cfclient or direct HTTP. The `cf curl` pattern parses CLI text output, which is fragile across CLI versions.

**Before:**

```go
output, err := conn.CliCommandWithoutTerminalOutput("curl", "/v3/apps?names=my-app")
// Parse JSON from output[0]
```

**After:**

```go
apps, err := cfClient.Applications.ListAll(ctx,
    &client.AppListOptions{Names: client.Filter{Values: []string{"my-app"}}})
```

Plugins that use `CliCommand` for workflow orchestration (`push`, `bind-service`, `restage`) may continue to do so during the transition, as these operations have no direct go-cfclient equivalent (they involve multi-step workflows managed by the CLI).

### Step 6: Prepare for the V3 Interface

Once the V3 plugin interface is available, migration from the transitional pattern is minimal:

| Change | Transitional | V3 Interface |
|---|---|---|
| Entry point | `plugin.Start(myPlugin)` | `plugin.Start(myPlugin)` (unchanged) |
| Connection type | `plugin.CliConnection` | `pluginapi.PluginContext` |
| Client construction | `newCfClient(conn)` | `cfhelper.NewCfClient(ctx)` (companion package) |
| Context methods | `conn.AccessToken()` | `ctx.AccessToken()` (same signatures) |
| Domain methods | Already removed (using go-cfclient) | Not available (by design) |
| Metadata | `GetMetadata()` return value | Embedded `CF_PLUGIN_METADATA:` marker |

The most significant change is the metadata mechanism (embedded marker vs. runtime `GetMetadata()`). Plugin logic — the domain operations using go-cfclient — remains unchanged.

## Companion Package Design

The V3 RFC proposes a companion package at `code.cloudfoundry.org/cli-plugin-helpers/cfclient`. This package SHOULD be published ahead of the full V3 interface to support the transitional migration:

```go
package cfhelper

import (
    "code.cloudfoundry.org/cli/plugin"
    "github.com/cloudfoundry/go-cfclient/v3/client"
    "github.com/cloudfoundry/go-cfclient/v3/config"
)

// CfConnection extends the standard plugin.CliConnection with direct V3 access.
type CfConnection interface {
    plugin.CliConnection
    CfClient() *client.Client
}

// NewCfConnection wraps an existing plugin.CliConnection and provides a
// go-cfclient V3 client constructed from the connection's credentials.
func NewCfConnection(conn plugin.CliConnection) (CfConnection, error) {
    endpoint, err := conn.ApiEndpoint()
    if err != nil {
        return nil, err
    }
    token, err := conn.AccessToken()
    if err != nil {
        return nil, err
    }
    skipSSL, err := conn.IsSSLDisabled()
    if err != nil {
        return nil, err
    }
    cfg, err := config.New(endpoint,
        config.Token(token),
        config.SkipTLSValidation(skipSSL),
    )
    if err != nil {
        return nil, err
    }
    cfClient, err := client.New(cfg)
    if err != nil {
        return nil, err
    }
    return &cfConnection{CliConnection: conn, client: cfClient}, nil
}

type cfConnection struct {
    plugin.CliConnection
    client *client.Client
}

func (c *cfConnection) CfClient() *client.Client {
    return c.client
}
```

Note: Unlike the Rabobank library, this companion package does **not** reimplement V2 domain methods via V3. Plugins SHOULD either use go-cfclient directly for domain operations, or use the [generated V2 compatibility wrappers](#alternative-generated-v2-compatibility-wrappers) that populate only the fields a plugin declares it needs.

## Lessons from the Rabobank Implementation

The Rabobank `cf-plugins` library validates this approach but also reveals pitfalls to avoid:

### What Worked

- **Zero host changes.** The library wraps `plugin.CliConnection` and calls `plugin.Start()` — compatible with any CF CLI version.
- **Incremental adoption.** Consumer plugins adopt at their own pace — 2 of 4 Rabobank consumers use the library.
- **Two migration tiers.** `plugins.Start()` provides transparent V3 reimplementation; `plugins.Execute()` provides direct `CfClient()` access.

### Rabobank Caveats in Context

The Rabobank README lists several caveats. Some of these are **not actually limitations** — they are faithful representations of what V2 always provided. V2-era plugins were never coded to expect capabilities that only exist in V3:

| Rabobank Caveat | Actually a problem? | Explanation |
|---|---|---|
| Single buildpack only | **No.** | V2's `BuildpackUrl` was always a single string. No existing V2 plugin expects multiple buildpacks. The wrapper correctly returns the first buildpack, which is what V2 returned. |
| Single process type | **No.** | V2 had no concept of multiple process types. `Instances`, `Memory`, and `DiskQuota` always described a single process. The wrapper populates from the `web` process type, matching V2 behavior. Plugins that need multi-process data are V3-aware and should use go-cfclient V3 types directly. |
| `IsAdmin` always false | **Avoidable cost tradeoff.** | Rabobank skipped the UAA role query to reduce API calls. The [generated wrapper](#alternative-generated-v2-compatibility-wrappers) includes the query only if the plugin declares it needs `IsAdmin`. |
| No per-app stats in list | **Avoidable cost tradeoff.** | Rabobank omitted stats to avoid N+1 per-process calls. The generated wrapper includes stats calls only if the plugin declares it needs `RunningInstances`. |
| 11 API calls for `GetApp()` | **Avoidable.** | Rabobank populates every field unconditionally. The generated wrapper makes only the calls needed for declared fields (e.g., 1 call for `Name`+`Guid`+`State`). |

### Consumer Plugin Analysis: Was the Full Reimplementation Necessary?

The `cf-plugins` library reimplements **10 V2 domain methods** via V3 (`GetApp`, `GetApps`, `GetOrgs`, `GetSpaces`, `GetOrgUsers`, `GetSpaceUsers`, `GetServices`, `GetService`, `GetOrg`, `GetSpace`). Source analysis of all 4 consumer plugins reveals that this was far more work than needed:

| Plugin | Uses cf-plugins? | V2 Domain Methods Called | Fields Accessed |
|---|---|---|---|
| scheduler-plugin | **No** | None | N/A — uses `go-cfclient/v3` directly |
| npsb-plugin | **No** | None | N/A — uses direct HTTP with `AccessToken()` |
| idb-plugin | Yes (`Execute()`) | None | N/A — uses `CfClient()` for V3 access |
| credhub-plugin | Yes (`Start()`) | **`GetService()` only** | `.Guid`, `.ServiceOffering.Name`, `.LastOperation.State` |

**Key findings:**

1. **Only 1 of 10 reimplemented methods is called** by any consumer plugin. `GetService()` is called by credhub-plugin; the other 9 reimplementations (`GetApp`, `GetApps`, `GetOrgs`, `GetSpaces`, `GetOrgUsers`, `GetSpaceUsers`, `GetServices`, `GetOrg`, `GetSpace`) are unused.

2. **Only 3 fields are accessed** from that single method call. Rabobank's `GetService()` reimplementation populates every field in `GetService_Model` — but credhub-plugin reads only `Guid`, `ServiceOffering.Name`, and `LastOperation.State`.

3. **2 of 4 consumer plugins don't use the library at all.** scheduler-plugin and npsb-plugin import the standard `plugin.Start()` and use their own `go-cfclient/v3` or direct HTTP for CAPI access.

4. **idb-plugin uses `Execute()` for `CfClient()` only** — it calls V3 APIs directly and never touches any reimplemented V2 method.

**What the generated wrapper approach would produce for credhub-plugin:**

```yaml
# credhub-plugin/cf-plugin-migrate.yml
methods:
  GetService:
    fields: [Guid, ServiceOffering.Name, LastOperation.State]
```

This would generate a wrapper making **2 V3 API calls** (`ServiceInstances.Single()` + `ServicePlans.Get()`) instead of Rabobank's full reimplementation. The remaining 9 V2 methods would not be reimplemented at all.

**This validates the generated wrapper approach:** build only what you use, not a complete V2-over-V3 compatibility layer. The Rabobank library spent significant implementation effort on methods that no consumer plugin calls.

### Implementation Bugs to Avoid

1. **Do not strip the token prefix manually.** Rabobank uses `token[7:]` to strip `"bearer "`. The go-cfclient `config.Token()` function handles this internally. Manual stripping is fragile if the prefix format ever changes.

2. **Do not hardcode SSL settings.** Rabobank calls `config.SkipTLSValidation()` unconditionally. The companion package MUST pass through the host's `IsSSLDisabled()` value.

3. **Do not hardcode user agent strings.** Rabobank uses `config.UserAgent("cfs-plugin/1.0.9")`. The companion package SHOULD derive the user agent from the plugin's metadata (name and version).

## Implementation Checklist

The migration guide above describes the *what*. This section captures the concrete work needed to make the transitional approach production-ready.

### Dependency Management

- **go-cfclient/v3 version guidance.** The library is still at alpha (surveyed plugins use alpha.9 through alpha.19). The transitional RFC SHOULD recommend a minimum alpha version and document any breaking changes between alphas.
- **CLI SDK version pinning.** Most plugins import `code.cloudfoundry.org/cli v7.1.0+incompatible`; a few use `code.cloudfoundry.org/cli/v8`. The transitional approach does not change this — both work.
- **CF API version floor.** go-cfclient/v3 requires CAPI V3 endpoints. The minimum CAPI version that supports all V3 resources used by the generated wrappers SHOULD be documented.

### Build System Integration

- **`//go:generate` directive** for the generated V2 compatibility wrappers. Plugins add a single line (e.g., `//go:generate cf-plugin-migrate generate`) to trigger regeneration.
- **Makefile target** (e.g., `make generate`) for plugins that use Make-based builds.
- **Generated file placement.** The generated file SHOULD live alongside the plugin source (e.g., `v2compat_generated.go`) and SHOULD be checked into version control so that `go install` works without the generator tool.

### Test Migration

- **Existing mocks survive.** Tests that mock `plugin.CliConnection` continue to work for context methods (`AccessToken`, `GetCurrentSpace`, etc.).
- **New domain tests mock go-cfclient.** Plugins that switch to go-cfclient V3 for domain operations need either a `*httptest.Server` or a mock client. go-cfclient provides `fake.Client` in its test package.
- **Generated wrapper tests.** The generator SHOULD produce a companion `_test.go` file with table-driven tests that verify correct field population against a test server.

### Token Lifecycle

The `AccessToken()` method returns a *snapshot* token — go-cfclient does not get a refresh token. For short-lived operations this is fine, but long-running plugins risk token expiry.

**Recommended pattern:** Pass a token provider function instead of a static token:

```go
func tokenProvider(conn plugin.CliConnection) func() (string, error) {
    return func() (string, error) {
        return conn.AccessToken()
    }
}
```

go-cfclient's `config.TokenProvider()` option accepts this pattern, re-fetching from the host's RPC each time the client needs a token. This is how the App Autoscaler plugin's token expiry pain point SHOULD be resolved.

### `cf-plugin-migrate` Generator Tool

The generated V2 compatibility wrapper approach requires a code generator:

- **YAML schema** for the `cf-plugin-migrate.yml` configuration (methods, fields, output path)
- **Generator implementation** that reads the config, maps fields to V3 API calls, and emits Go source
- **Packaging** — standalone CLI tool, installable via `go install`. Proposed location: `code.cloudfoundry.org/cli-plugin-helpers/cmd/cf-plugin-migrate`
- **Field-to-API-call mapping** maintained as a data file in the generator, updatable as go-cfclient evolves

### Error Handling at the Boundary

The transitional wrapper sits between the host's RPC interface and the V3 API. Both sides can fail:

| Failure | Source | Recommended Handling |
|---|---|---|
| Empty API endpoint | `ApiEndpoint()` returns `""` | Fail fast: `"no API target — run 'cf api' first"` |
| Empty access token | `AccessToken()` returns `""` | Fail fast: `"not logged in — run 'cf login' first"` |
| Token expired during V3 call | go-cfclient returns 401 | Use `config.TokenProvider()` to re-fetch from host |
| V3 endpoint not available | go-cfclient returns 404 | Report minimum CAPI version required |
| Host RPC disconnected | `CliConnection` method returns error | Fail with context: the host (CLI) may have exited |

### Backward Compatibility

- **CLI version compatibility.** The transitional approach works with any CF CLI that implements `plugin.CliConnection` (v6.16+, v7, v8, v9). No host changes are required.
- **CAPI version compatibility.** go-cfclient/v3 requires CAPI V3 endpoints. Foundations running CF API v3.x (CF Deployment 1.x+) are supported. The generated wrappers SHOULD document the minimum CAPI version per V3 resource used.

## Proof-of-Concept Candidates

To validate the transitional approach, three plugins from the [plugin survey](plugin-survey.md) represent increasing levels of migration complexity:

### Tier 1: Simple — list-services

- **V2 methods used:** `GetApp()` — for GUID resolution only
- **Fields consumed:** `Guid` only (1 field → 1 V3 API call)
- **Migration:** Replace `conn.GetApp(name)` with `cfClient.Applications.Single(ctx, opts)`, read `.GUID` — or generate a wrapper that returns `GetAppModel{Guid: app.GUID}`
- **Why:** Simplest possible case. Validates the end-to-end flow with minimal risk.

### Tier 2: Moderate — OCF Scheduler

- **V2 methods used:** `GetApp()`, `GetApps()`
- **Fields consumed:** `Name`, `Guid`, `State` (3 fields → 1 V3 API call each)
- **Migration:** Two methods to replace, both using only core app fields available from a single `Applications` call
- **Why:** Actively maintained, representative of the common pattern. Already uses direct HTTP for scheduler operations — only the app lookup needs migration.

### Tier 3: Complex — metric-registrar

- **V2 methods used:** `GetApp()`, `GetApps()`, `GetServices()`
- **Additional V2 dependency:** `cf curl /v2/user_provided_service_instances`, `/v2/apps/{guid}`
- **Fields consumed:** Multiple fields across app and service models
- **Migration:** Requires both generated wrappers (for V2 model methods) and `cf curl` replacement (for V2 CAPI endpoints). Tests the full migration path.
- **Why:** Most V2-coupled active plugin. If the transitional approach works here, it works everywhere.

### Secondary Candidates

| Plugin | V2 Methods | Notes |
|---|---|---|
| spring-cloud-services | `GetService()`, `GetApps()` | Clean architecture, good test of service model migration |
| stack-auditor | `GetOrgs()` + `cf curl /v2/...` | Tests migration of both model methods and V2 curl calls |
| service-instance-logs | `GetService()` | V2 chain traversal (service → plan → service offering) — complex V3 mapping |

## Relationship to the V3 Plugin Interface RFC

This transitional approach is **Phase 0** — work that plugin developers can do immediately, before any host changes ship. The V3 RFC migration phases build on top of this:

| Phase | Timeline | What Changes | Guest Action |
|---|---|---|---|
| **Phase 0: Transitional** | Now | Nothing (guest-side only) | Replace V2 domain methods with go-cfclient; adopt companion package pattern |
| **Phase 1: Channel Abstraction** | Q3 2026 | Host adds `PluginChannel` interface, embedded metadata scanning | Add `CF_PLUGIN_METADATA:` marker to binary |
| **Phase 2: JSON-RPC** | Q4 2026 | Host supports JSON-RPC protocol | Adopt JSON-RPC if polyglot support needed |
| **Phase 3: Deprecation** | Q1 2027 | Host emits warnings for legacy guests | Verify no legacy method usage remains |
| **Phase 4: Removal** | Q3 2027+ | Legacy gob/net-rpc removed | Already migrated |

Plugins that complete Phase 0 will have minimal work remaining for Phases 1–4, since their domain logic already uses go-cfclient directly and their host interaction is limited to the core context methods that the V3 interface preserves.

## References

- [CLI Plugin Interface V3 RFC](rfc-draft-cli-plugin-interface-v3.md) — The main RFC defining the new plugin interface
- [Plugin Survey — Rabobank Case Study](plugin-survey.md#case-study-rabobank-guest-side-transitional-wrapper) — Detailed analysis of the Rabobank transitional wrapper
- [Rabobank cf-plugins](https://github.com/rabobank/cf-plugins) — The production transitional wrapper library
- [go-cfclient](https://github.com/cloudfoundry/go-cfclient) — Cloud Foundry V3 Go client library
- [cloudfoundry/cli#3621](https://github.com/cloudfoundry/cli/issues/3621) — New Plugin Interface tracking issue
