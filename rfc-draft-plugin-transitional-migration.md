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

**Version note:** go-cfclient v3 is still in alpha (v3.0.0-alpha.20 as of March 2026). See [go-cfclient V3 Version Guidance](#go-cfclient-v3-version-guidance) for minimum version recommendations and stability considerations.

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

#### Automated Audit: `cf-plugin-migrate scan`

Writing the YAML configuration by hand requires tracing every V2 domain method call and every field access — the same analysis we performed manually for the [OCF Scheduler](#worked-example-ocf-scheduler-plugin) and [metric-registrar](#worked-example-metric-registrar-plugin-complex-migration) worked examples. A `scan` subcommand automates this using Go's `go/ast` package.

**Usage:**

```bash
cf-plugin-migrate scan ./...          # → produces cf-plugin-migrate.yml
cf-plugin-migrate generate            # → produces v2compat_generated.go
```

**What the scanner does:**

1. **Finds call sites** — walks the AST for method calls matching `*.GetApp(`, `*.GetApps(`, `*.GetService(`, `*.GetServices(`, `*.GetOrg(`, `*.GetOrgs(`, `*.GetSpaces(` on variables typed as `plugin.CliConnection` or any interface embedding it
2. **Traces field access** — follows the return value through assignments and identifies which fields are accessed (`.Guid`, `.Name`, `.Routes[].Host`, etc.)
3. **Outputs the YAML** — methods and their accessed fields, ready for the generator

**Coverage tiers:**

| Pattern | Example | Scanner Handles? |
|---|---|---|
| Direct field access | `app, _ := conn.GetApp(name); app.Guid` | **Yes** — straightforward AST pattern |
| Iteration with field access | `for _, s := range services { s.Name }` | **Yes** — tracks range variable type |
| Passed to helper function | `core.AppByGUID(apps, guid)` then `.Name` in caller | **Flagged for review** — cross-function data flow |
| Stored in struct field | `svc.App = app` then `svc.App.Guid` later | **Flagged for review** — requires alias tracking |
| Reflection / interface cast | `reflect.ValueOf(app).FieldByName("Guid")` | **No** — not expected in practice |

The scanner does not need to be perfect. A conservative approach that identifies definitely-used fields and flags ambiguous cases for manual review is sufficient. The manual cases (cross-function, struct storage) are uncommon — the OCF Scheduler and metric-registrar analyses show that most plugins access fields directly at or near the call site.

**Example output for OCF Scheduler:**

```bash
$ cf-plugin-migrate scan ./...
Scanning ./...

Found V2 domain method calls:
  commands/create-job.go:42   conn.GetApp(appName)    → fields: .Guid
  commands/create-call.go:38  conn.GetApp(appName)    → fields: .Guid
  core/util.go:15             conn.GetApps()          → fields: .Guid, .Name

Writing cf-plugin-migrate.yml
```

```yaml
# cf-plugin-migrate.yml (generated by scan)
package: main
methods:
  GetApp:
    fields: [Guid]
  GetApps:
    fields: [Guid, Name]
```

**Example output with flagged cases (metric-registrar):**

```bash
$ cf-plugin-migrate scan ./...
Scanning ./...

Found V2 domain method calls:
  command/register.go:87      conn.GetApp(appName)    → fields: .Guid, .Name, .Routes
  command/unregister.go:45    conn.GetApp(appName)    → fields: .Guid
  command/unregister.go:92    conn.GetApp(appName)    → fields: .Guid
  command/list.go:34          conn.GetApps()          → fields: .Guid, .Name
  command/list.go:67          conn.GetApps()          → fields: .Guid, .Name
  command/register.go:112     conn.GetServices()      → fields: .Name

Routes sub-fields accessed:
  command/register.go:95      .Routes[].Host, .Routes[].Domain.Name,
                              .Routes[].Port, .Routes[].Path

Writing cf-plugin-migrate.yml
```

#### Migration Path with Generated Wrappers

1. **Scan** — Run `cf-plugin-migrate scan ./...` to auto-detect V2 domain method calls and field usage, producing `cf-plugin-migrate.yml`. Review any flagged cases manually.
2. **Review** — Check the generated YAML for completeness. Add any fields the scanner flagged but couldn't resolve.
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

### Consumer Plugin Analysis: Historical and Current V2 Usage

The `cf-plugins` library reimplements **10 V2 domain methods** via V3. To evaluate whether this scope was justified, we analyzed both the current state and the git history of all 4 consumer plugins.

#### Historical V2 Domain Method Usage

| Plugin | V2 Methods Used Historically | Duration | How They Migrated | Key Commits |
|---|---|---|---|---|
| scheduler-plugin | `GetServices()`, `GetService()`, `GetApp()` | Feb 2023 – Oct 2025 (2.5 years) | Migrated directly to go-cfclient/v3 — did **not** adopt cf-plugins | First: [`57130bdb`](https://github.com/rabobank/scheduler-plugin/commit/57130bdb), Migration: [`e682b800`](https://github.com/rabobank/scheduler-plugin/commit/e682b800) |
| credhub-plugin | `GetService()` | Aug 2023 – Oct 2025 (2+ years) | Adopted cf-plugins — **2-line change** in `main.go` | First: [`e5355478`](https://github.com/rabobank/credhub-plugin/commit/e5355478), Migration: [`7cdaded9`](https://github.com/rabobank/credhub-plugin/commit/7cdaded9) |
| npsb-plugin | None (context methods only, from day one) | N/A | No migration needed | First: [`ef0c3e10`](https://github.com/rabobank/npsb-plugin/commit/ef0c3e10) |
| idb-plugin | None (built with cf-plugins from the start, Oct 2025) | N/A | Born V3-native via `CfClient()` | First: [`938005ae`](https://github.com/rabobank/idb-plugin/commit/938005ae) |

The cf-plugins library was created **Oct 2, 2025** ([`a0486ef6`](https://github.com/rabobank/cf-plugins/commit/a0486ef6)), with its CliConnection wrapper landing Oct 13 ([`0443494a`](https://github.com/rabobank/cf-plugins/commit/0443494a)) and enhanced `Execute` support Oct 14 ([`dbaad4c8`](https://github.com/rabobank/cf-plugins/commit/dbaad4c8)). The scheduler-plugin's migration commit message is explicit: *"remove dependency on some cliConnection calls since they still require cf v2 api"*. Commit authorship confirms the same developer migrated the scheduler-plugin and then created the cf-plugins library, generalizing the migration pattern for other consumers.

#### The Library Solved a Real Problem

The credhub-plugin migration demonstrates the library's value: a 2-line change (`plugin.Start()` → `plugins.Start()`) transparently replaced V2 RPC-backed domain method calls with V3 API calls. Without the library, credhub-plugin would have needed the kind of larger refactoring the scheduler-plugin did.

#### But the Scope Was Broader Than Needed

Across all 4 consumer plugins, historically and currently, only **3 of 10** reimplemented methods were ever called:

| Reimplemented Method | Ever Called By | Fields Accessed |
|---|---|---|
| `GetApp()` | scheduler-plugin (historical, now migrated away) | `.Guid` only |
| `GetService()` | credhub-plugin (current), scheduler-plugin (historical) | `.Guid`, `.ServiceOffering.Name`, `.LastOperation.State` |
| `GetServices()` | scheduler-plugin (historical, now migrated away) | `.Name`, `.ServiceOffering.Name` |
| `GetApps()` | Never called | — |
| `GetOrg()` | Never called | — |
| `GetOrgs()` | Never called | — |
| `GetSpace()` | Never called | — |
| `GetSpaces()` | Never called | — |
| `GetOrgUsers()` | Never called | — |
| `GetSpaceUsers()` | Never called | — |

The library reimplemented 10 methods, but only 3 were ever used — and those 3 accessed a combined total of **5 unique fields**. The remaining 7 reimplementations were speculative, anticipating broader adoption that hasn't materialized.

#### Current State (Post-Migration)

| Plugin | Uses cf-plugins? | V2 Domain Methods Called Now | Fields Accessed Now |
|---|---|---|---|
| scheduler-plugin | **No** | None | N/A — uses `go-cfclient/v3` directly |
| npsb-plugin | **No** | None | N/A — uses direct HTTP with `AccessToken()` |
| idb-plugin | Yes (`Execute()`) | None | N/A — uses `CfClient()` for V3 access |
| credhub-plugin | Yes (`Start()`) | **`GetService()` only** | `.Guid`, `.ServiceOffering.Name`, `.LastOperation.State` |

Today, only 1 method is called by 1 plugin, accessing 3 fields.

#### What the Generated Wrapper Approach Would Have Produced

For the scheduler-plugin's historical usage:

```yaml
methods:
  GetApp:
    fields: [Guid]
  GetService:
    fields: [Guid, ServiceOffering.Name]
  GetServices:
    fields: [Name, ServiceOffering.Name]
```

For credhub-plugin's current usage:

```yaml
methods:
  GetService:
    fields: [Guid, ServiceOffering.Name, LastOperation.State]
```

Each would generate minimal wrappers with 1–2 V3 API calls per method, instead of the library's comprehensive reimplementation. The 7 unused methods would never have been written.

**Takeaway:** The cf-plugins library was a valid response to a real migration pressure, and the `plugins.Start()` pattern (transparent drop-in) was an elegant design. But the all-or-nothing reimplementation strategy — building every V2 method before knowing which ones consumers need — is exactly the waste the generated wrapper approach avoids. Build only what you use.

### Implementation Bugs to Avoid

1. **Do not strip the token prefix manually.** Rabobank uses `token[7:]` to strip `"bearer "`. The go-cfclient `config.Token()` function handles this internally. Manual stripping is fragile if the prefix format ever changes.

2. **Do not hardcode SSL settings.** Rabobank calls `config.SkipTLSValidation()` unconditionally. The companion package MUST pass through the host's `IsSSLDisabled()` value.

3. **Do not hardcode user agent strings.** Rabobank uses `config.UserAgent("cfs-plugin/1.0.9")`. The companion package SHOULD derive the user agent from the plugin's metadata (name and version).

## Implementation Checklist

The migration guide above describes the *what*. This section captures the concrete work needed to make the transitional approach production-ready.

### go-cfclient V3 Version Guidance

#### Library Status

go-cfclient v3 is published at `github.com/cloudfoundry/go-cfclient/v3`. As of March 2026, the latest release is **v3.0.0-alpha.20**. The library has shipped 20 alpha releases but no stable v3.0.0. The README states: *"The v3 version in the main branch is currently under development and may have breaking changes until a v3.0.0 release is cut."*

Despite the alpha label, the library is in production use by multiple CF CLI plugins and has near-complete CAPI V3 coverage.

#### CAPI V3 Coverage

go-cfclient v3 implements **31 of 35 CAPI V3 resource groups with full coverage**, 2 with partial coverage, and 1 missing. Every resource needed for plugin migration is fully supported:

| Resource Group | Coverage | Notes |
|---|---|---|
| Apps, Processes, Builds, Droplets, Packages | Full | Includes `PollStaged`, `PollReady` utilities |
| Routes, Route Destinations | Full | `InsertDestinations`, `ReplaceDestinations`, `RemoveDestination` |
| Service Instances (managed + user-provided) | Full | `CreateManaged`, `CreateUserProvided`, sharing support |
| Service Credential Bindings | Full | Includes `GetDetails`, `GetParameters` |
| Service Plans, Offerings, Brokers | Full | Includes `Include` variants for eager loading |
| Service Route Bindings | Full | |
| Organizations, Spaces, Roles, Users | Full | Includes `Include` variants for eager loading |
| Domains, Stacks, Security Groups | Full | |
| Tasks, Deployments, Sidecars | Full | |
| Isolation Segments, Quotas (org + space) | Full | |
| Feature Flags, Manifests, Resource Matches | Full | |
| Audit Events, Usage Events (app + service) | Full | |
| Revisions, Buildpacks, Jobs | Full | `PollComplete` for async operations |
| Info (`/v3/info`) | **None** | Platform metadata — rarely needed by plugins |
| Root (`/`, `/v3`) | Partial | Missing `/v3/info` and `/v3/usage_summary` |
| Space Features | Partial | Only SSH; generic feature pattern not exposed |

The library adds value beyond raw CAPI coverage:
- **Pagination:** `ListAll` (auto-pages), `First`, `Single` helpers
- **Include-based eager loading:** e.g., list apps with their spaces and orgs in one call
- **Async job polling:** `PollComplete`, `PollStaged`, `PollReady`
- **Typed filters:** `AppListOptions`, `ServiceInstanceListOptions`, etc.
- **CF error codes:** typed predicates for error handling

#### Versions in Production Use

Surveyed plugins pin to different alpha versions:

| Plugin | go-cfclient Version | Date |
|---|---|---|
| cf-lookup-route | v3.0.0-alpha.9 | 2024 |
| Rabobank cf-plugins | v3.0.0-alpha.15 | 2025 |
| DefaultEnv | v3.0.0-alpha.17 | 2025 |
| App Autoscaler | v3.0.0-alpha.19 | 2026 |

The spread from alpha.9 to alpha.19 indicates breaking changes between versions that forced plugins to pin rather than track latest.

#### Minimum Version Recommendation

Plugins adopting the transitional approach SHOULD use **v3.0.0-alpha.17 or later**. This version:
- Includes the `config.Token()` function that handles the `"bearer "` prefix internally
- Supports `config.SkipTLSValidation()` with a boolean parameter
- Has stable interfaces for all resources used by the generated wrappers (Apps, Routes, Service Instances, Service Credential Bindings, Service Plans, Service Offerings)

Plugins SHOULD pin to a specific alpha version in `go.mod` and upgrade deliberately, testing for breaking changes. Once go-cfclient releases v3.0.0 stable, all plugins SHOULD upgrade to it.

#### CF API Version Floor

go-cfclient v3 requires CAPI V3 endpoints. The minimum CF API version depends on which resources the plugin uses:

| Resource | Minimum CAPI Version | CF Deployment |
|---|---|---|
| Apps, Spaces, Orgs (core) | 3.0.0 | 1.0+ |
| Service Instances (user-provided) | 3.0.0 | 1.0+ |
| Service Credential Bindings | 3.77.0 | ~18.0+ |
| Route Destinations | 3.77.0 | ~18.0+ |
| Service Plans, Offerings | 3.77.0 | ~18.0+ |

Most actively maintained CF foundations run CAPI 3.100+ (CF Deployment 25+), so the version floor is not a practical concern for current deployments. The generated wrappers SHOULD document the minimum CAPI version per V3 resource used.

### UAA and CredHub

The CAPI V2→V3 transition does not affect UAA or CredHub interfaces. Survey analysis confirms that plugin interaction with these services is minimal and falls outside the plugin interface contract:

- **UAA:** Only 1 of 18 surveyed plugins (html5-apps-repo) calls UAA directly — a `client_credentials` token exchange for service-specific access. All other plugins consume UAA-issued tokens exclusively through the host's `AccessToken()` method.
- **CredHub:** Only 1 plugin (credhub-plugin) talks to a CredHub service broker API, using CAPI to resolve the broker URL. It does not interact with the CredHub credential store directly.

The core contract's `AccessToken()` covers plugin developer needs. Plugins requiring service-specific tokens or credential store access handle that themselves outside the plugin interface, and those patterns are unaffected by the CAPI V2→V3 migration.

### Dependency Management

- **CLI SDK version pinning.** Most plugins import `code.cloudfoundry.org/cli v7.1.0+incompatible`; a few use `code.cloudfoundry.org/cli/v8`. The transitional approach does not change this — both work.
- **Dependency tree impact.** Adding go-cfclient/v3 pulls in `golang.org/x/oauth2`, `google.golang.org/protobuf`, and several other transitive dependencies. For plugins that vendor dependencies, this increases the vendor directory. For plugins using module proxies, the impact is minimal.

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

### `cf-plugin-migrate` Tool

The tool has two subcommands — `scan` (audit) and `generate` (code generation):

```
cf-plugin-migrate scan ./...          # AST-based audit → cf-plugin-migrate.yml
cf-plugin-migrate generate            # YAML config → v2compat_generated.go
```

**Implementation scope:**

- **`scan`** — Uses `go/ast` and `go/types` to find V2 domain method call sites, trace field access on return values, and emit the YAML config. Flags cross-function data flow for manual review. See [Automated Audit](#automated-audit-cf-plugin-migrate-scan) for design details.
- **`generate`** — Reads the YAML config, maps fields to V3 API calls using a field dependency table, and emits Go source with typed go-cfclient calls. See [How It Works](#how-it-works) and [Field-to-API-Call Mapping](#field-to-api-call-mapping) for the generation model.
- **YAML schema** for `cf-plugin-migrate.yml` — methods, fields, route sub-fields, output package name
- **Field-to-API-call mapping** maintained as a data table in the tool, updatable as go-cfclient evolves
- **Packaging** — standalone CLI tool, installable via `go install`. Proposed location: `code.cloudfoundry.org/cli-plugin-helpers/cmd/cf-plugin-migrate`

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

## Future Work: CAPI OpenAPI Specification and Polyglot Clients

### The Missing Machine-Readable API Spec

CAPI V3 has **no official OpenAPI or Swagger specification**. The V3 API docs at [v3-apidocs.cloudfoundry.org](https://v3-apidocs.cloudfoundry.org/) are generated from hand-written Slate Markdown, not from a machine-readable spec. This has been an open request since 2015, tracked as [cloud_controller_ng#2192](https://github.com/cloudfoundry/cloud_controller_ng/issues/2192) since 2021. A proof-of-concept to generate OpenAPI from CAPI's Ruby source code ([PR #4500](https://github.com/cloudfoundry/cloud_controller_ng/pull/4500)) was closed without merging.

As a result, all existing CAPI client libraries — Go, Java, Python — are **hand-written**, not generated from a spec.

### Community OpenAPI Efforts

| Project | Approach | Status |
|---|---|---|
| [capi-openapi-spec](https://github.com/cloudfoundry-community/capi-openapi-spec) (cloudfoundry-community) | Parses V3 HTML docs → OpenAPI 3.0.0. Claims 100% coverage of 44 resource types (CAPI v3.195.0). | Active (June 2025) |
| [capi-openapi-go-client](https://github.com/cloudfoundry-community/capi-openapi-go-client) (cloudfoundry-community) | Go client generated from the above spec via oapi-codegen | Active (June 2025) |
| [cf-api-openapi-poc](https://github.com/FloThinksPi/cf-api-openapi-poc) | Manual + AI-assisted conversion → OpenAPI 3.1.0 | POC (July 2025) |
| [SAP OpenAPI contribution](https://github.com/sap-contributions/cloudfoundry-cloud-controller-v3-openapi) | Manual spec file | Stale (Nov 2023) |

### CAPI V3 Client Libraries Across Languages

| Library | Language | Maintained? | Notes |
|---|---|---|---|
| [go-cfclient](https://github.com/cloudfoundry/go-cfclient) | Go | Yes (official, `cloudfoundry` org) | Hand-written, v3.0.0-alpha.20, recommended by this RFC |
| [cf-java-client](https://github.com/cloudfoundry/cf-java-client) | Java | Yes (official, `cloudfoundry` org) | Hand-written, Reactor Netty-based, 330 stars |
| [cf-python-client](https://github.com/cloudfoundry-community/cf-python-client) | Python | Yes (community) | Hand-written, 55 stars |
| [capi-openapi-go-client](https://github.com/cloudfoundry-community/capi-openapi-go-client) | Go | Active (community) | Generated from OpenAPI spec, less mature than go-cfclient |

### Implications for Plugin Migration

1. **Go plugins** can use go-cfclient (recommended) or cf-java-client is available for JVM-based plugins. Python plugins can use cf-python-client.

2. **Polyglot gap.** The V3 RFC proposes JSON-RPC for polyglot plugin support, but plugin authors in languages beyond Go, Java, and Python have no CAPI client SDK today. An official OpenAPI spec would enable generated clients in any language via standard code generators (openapi-generator, oapi-codegen, etc.).

3. **The `capi-openapi-spec` project under `cloudfoundry-community`** is the most promising path toward an official machine-readable spec. If it matures and is adopted by the CF Foundation, it would significantly strengthen the polyglot plugin story by enabling auto-generated CAPI clients in any language.

4. **This RFC recommends go-cfclient** as the companion library for Go plugins. The OpenAPI-generated client is an alternative but lacks go-cfclient's convenience methods (polling, eager loading, `Single`/`First` helpers). As both mature, a future recommendation update may be warranted.

## References

- [CLI Plugin Interface V3 RFC](rfc-draft-cli-plugin-interface-v3.md) — The main RFC defining the new plugin interface
- [Plugin Survey — Rabobank Case Study](plugin-survey.md#case-study-rabobank-guest-side-transitional-wrapper) — Detailed analysis of the Rabobank transitional wrapper
- [Rabobank cf-plugins](https://github.com/rabobank/cf-plugins) — The production transitional wrapper library
- [go-cfclient](https://github.com/cloudfoundry/go-cfclient) — Cloud Foundry V3 Go client library
- [cf-java-client](https://github.com/cloudfoundry/cf-java-client) — Cloud Foundry Java client library
- [cf-python-client](https://github.com/cloudfoundry-community/cf-python-client) — Cloud Foundry Python client library
- [capi-openapi-spec](https://github.com/cloudfoundry-community/capi-openapi-spec) — Community-maintained OpenAPI 3.0.0 spec for CAPI V3
- [cloud_controller_ng#2192](https://github.com/cloudfoundry/cloud_controller_ng/issues/2192) — Tracking issue for official CAPI OpenAPI spec
- [cloudfoundry/cli#3621](https://github.com/cloudfoundry/cli/issues/3621) — New Plugin Interface tracking issue
