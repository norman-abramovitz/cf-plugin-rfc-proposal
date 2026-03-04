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
