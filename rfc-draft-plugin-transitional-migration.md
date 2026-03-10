# Meta
[meta]: #meta
- Name: CLI Plugin Transitional Migration Guide
- Start Date: 2026-03-01
- Author(s): @norman-abramovitz
- Status: Draft
- Tracking Issue: [cloudfoundry/cli#3621](https://github.com/cloudfoundry/cli/issues/3621)

## Summary

**Key terms used throughout this document:**
- **Host** — the CF CLI process that launches and manages plugins
- **Guest** — the plugin process launched by the host
- **V2 domain methods** — the 10 methods on `plugin.CliConnection` that return V2-shaped data (`GetApp`, `GetApps`, `GetService`, `GetServices`, `GetOrg`, `GetOrgs`, `GetSpace`, `GetSpaces`, `GetOrgUsers`, `GetSpaceUsers`)
- **Context methods** — the 11 methods on `plugin.CliConnection` that return session and authentication data (`AccessToken`, `ApiEndpoint`, `GetCurrentOrg`, `GetCurrentSpace`, `Username`, `IsLoggedIn`, `HasOrganization`, `HasSpace`, `IsSSLDisabled`, `HasAPIEndpoint`, `ApiVersion`)
- **CAPI V3** — Cloud Controller API version 3, the CF platform's REST API
- **go-cfclient** — the official Go client library for CAPI V3 (`github.com/cloudfoundry/go-cfclient/v3`)
- **`plugin.CliConnection`** — the Go interface that the host provides to guests, carrying both V2 domain methods and context methods

This document describes a **guest-side (plugin-side) transitional migration approach** that allows existing Go plugins to begin migrating from V2 domain methods to direct CAPI V3 access **today**, without waiting for any host changes.

The approach has two key properties:

1. **No host changes required.** A generated wrapper library sits entirely on the guest side, wraps the existing `plugin.CliConnection`, and provides a pre-configured go-cfclient V3 client. It works with any existing CF CLI version (v7, v8, v9).

2. **Host-guest separation.** By moving domain operations to the guest side, the host CLI is freed to remove its V2 domain method implementation code (`GetApp`, `GetApps`, `GetService`, etc.) on its own timeline. The guest no longer depends on the host for V2 data — it fetches directly from CAPI V3. This clean separation benefits both sides: plugin developers migrate at their own pace, and CLI maintainers can simplify the host codebase without coordinating with every plugin author.

The migration is supported by **`cf-plugin-migrate`**, a companion tool that scans plugin source code, detects V2 usage, and generates minimal V3-backed wrapper functions. For session-only plugins (those that use only context methods like `AccessToken`, `GetCurrentOrg`, etc.), the migration requires **zero code changes** — just drop in the generated file. For plugins that call V2 domain methods, the migration requires **one new dependency** (go-cfclient/v3) and optionally **one line of code** changed.

This approach is validated by the [Rabobank `cf-plugins`](https://github.com/rabobank/cf-plugins) library, which has been in production use since 2025 (see [plugin survey case study](plugin-survey.md#case-study-rabobank-guest-side-transitional-wrapper)).

## Problem

### CAPI V2 Is Reaching End of Life

Plugins that depend on V2-shaped data from the host's domain methods (`GetApp`, `GetApps`, `GetService`, etc.) will stop working when V2 endpoints are removed. The current plugin interface returns `plugin_models.*` types that mirror V2 response structures — these types cannot represent V3 concepts like multiple process types, sidecars, rolling deployments, service credential bindings, or metadata labels.

### The Host Carries Legacy Code for Plugin Support

The CF CLI host currently implements 10 V2 domain methods on behalf of plugins via RPC. Each method involves:
- An RPC handler in `plugin/rpc/cli_rpc_server.go`
- V2-shaped response types in `plugin/models/`
- Config-derived accessors and state management

This code exists solely to serve the guest-side plugin interface. Once plugins fetch their own data via CAPI V3, the host can remove this entire subsystem — reducing maintenance burden, simplifying the CLI codebase, and eliminating a class of RPC-related bugs.

### CliCommand Is a Fragile Escape Hatch

Many plugins use `CliCommand` or `CliCommandWithoutTerminalOutput` to run arbitrary CF CLI commands, including `cf curl` for direct CAPI access. These calls:
- Parse CLI text output, which is fragile across CLI versions
- Cannot be statically analyzed by the host for deprecation planning
- Mix workflow orchestration (e.g., `push`, `bind-service`) with data access (e.g., `curl /v2/apps`)

A scan of 18 actively maintained plugins found `CliCommand` usage across 14 plugins, with patterns ranging from simple `cf apps` to complex `cf curl` with JSON parsing and pagination.

### Plugins Import CLI Internal Packages

Beyond the intended public interface (`plugin/` and `plugin/models/`), 8 of 18 surveyed plugins import internal CLI packages — creating a build-time dependency on code the CLI team never intended to expose.

An audit of all 18 plugins (performed against upstream default branches, not local work branches) found two dominant coupling patterns:

| Internal Package | Plugins | Purpose |
|---|---|---|
| `cf/terminal` | 6 | Colored/formatted terminal output |
| `cf/trace` | 6 | HTTP request tracing/debug logging |
| `cf/configuration/confighelpers` | 4 | Config file path discovery |
| `cf/i18n` | 2 | Internationalization |
| Other (`cf/formatters`, `cf/flags`, `util/configv3`, `util/ui`) | 1 each | Formatting, flag parsing, config access, UI rendering |

**Coupled plugins:**

| Plugin | Internal Packages | Severity |
|---|---|---|
| MultiApps / MTA | `cf/terminal`, `cf/formatters`, `cf/i18n`, `cf/trace` | High — 14+ production files |
| mysql-cli-plugin | `cf/configuration/confighelpers`, `util/configv3`, `util/ui` | High — deepest internal coupling |
| App Autoscaler | `cf/trace`, `cf/configuration/confighelpers` | Medium |
| cf-targets-plugin | `cf/configuration`, `cf/configuration/confighelpers`, `cf/configuration/coreconfig` | Medium — config file read/write |
| Swisscom appcloud | `cf/flags`, `cf/terminal`, `cf/trace` | Medium |
| cf-java-plugin | `cf/terminal`, `cf/trace` | Low |
| html5-apps-repo | `cf/terminal`, `cf/i18n` | Low |
| list-services | `cf/terminal`, `cf/trace` | Low |

**These packages are currently frozen — but that's luck, not design.** Analysis of the CF CLI git history shows zero exported API changes in `cf/configuration/confighelpers` since 2020, and only test infrastructure updates (ginkgo v2) in `cf/terminal`, `cf/trace`, `cf/formatters`, `cf/i18n`, and `cf/flags`. The coupling hasn't broken plugins *yet* because the CLI team hasn't refactored these packages. Any future refactoring of `cf/terminal` or `cf/configuration` would break 8 plugins with no warning.

The one exception is `util/configv3` (mysql-cli-plugin only), which has 24 commits since 2020 including structural changes for Kubernetes support — this coupling has already diverged and would not compile against CLI HEAD.

The module path migration from `code.cloudfoundry.org/cli` to `code.cloudfoundry.org/cli/v8` is an additional breaking change for plugins still pinned to `v7.1.0+incompatible`.

**Why this matters for the transitional migration:** The V2Compat wrapper approach addresses the *intended* coupling (imports of `plugin/` and `plugin/models/`). But plugins with internal package imports have a second, harder coupling problem that the wrapper cannot solve — they must replace those imports with standalone alternatives (e.g., standard library `log` instead of `cf/trace`, `text/tabwriter` or third-party packages instead of `cf/terminal`).

### Who Should Migrate

- Plugin developers whose plugins call V2 domain methods (`GetApp`, `GetApps`, `GetService`, `GetServices`, `GetOrg`, `GetOrgs`, `GetSpaces`, `GetOrgUsers`, `GetSpaceUsers`)
- Plugin developers who use `CliCommand`/`CliCommandWithoutTerminalOutput` for `cf curl` against CAPI endpoints
- Plugin developers who want to access CAPI V3 directly but currently bootstrap go-cfclient or custom HTTP clients manually

## Proposal

### Architecture

The transitional approach introduces a thin wrapper on the guest side:

```
┌─────────────────────────────────────────────────────┐
│                  Host (CF CLI)                       │
│                                                     │
│  Existing plugin.CliConnection via gob/net-rpc       │
│  (no changes required)                              │
└────────────────────────┬────────────────────────────┘
                         │
                         │  Existing gob/net-rpc protocol (Go's built-in binary RPC)
                         │
┌────────────────────────▼────────────────────────────┐
│                  Guest (Plugin)                      │
│                                                     │
│  ┌───────────────────────────────────────────────┐  │
│  │  V2Compat Wrapper (generated)                 │  │
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
- **Reimplements** only the V2 domain methods the plugin actually uses, backed by the minimum V3 API calls required
- **Satisfies** `plugin.CliConnection` — existing code that accepts the connection interface works without changes

**What changes vs. what doesn't:**

| Component | Changes? | Details |
|---|---|---|
| `plugin.Start()` entry point | No | Standard host registration, unchanged |
| Context methods (`AccessToken`, `GetCurrentOrg`, etc.) | No | Pass through to host via RPC |
| V2 domain methods (`GetApp`, `GetApps`, etc.) | **Yes** | Reimplemented via V3 on guest side |
| `CliCommand` / `CliCommandWithoutTerminalOutput` | **Flagged** | Should migrate to go-cfclient; workflow commands can stay |
| Host CLI codebase | No | No host changes required |

### Host-Guest Separation Enables CLI Simplification

A key benefit of this migration is that it **decouples the guest's data needs from the host's implementation**. Today, when a plugin calls `conn.GetApp("myapp")`, the host must:

1. Receive the RPC call
2. Query CAPI (currently V2) on behalf of the plugin
3. Populate a `plugin_models.GetAppModel` struct
4. Serialize and return it via gob/net-rpc

After migration, the guest calls CAPI V3 directly. The host's role shrinks to providing authentication context (`AccessToken`, `ApiEndpoint`, `IsSSLDisabled`) and session state (`GetCurrentOrg`, `GetCurrentSpace`). These are simple, stable methods unlikely to change.

This means the CLI team can:
- Remove the V2 domain method RPC handlers from `plugin/rpc/cli_rpc_server.go`
- Remove the `plugin_models.*` types that mirror V2 response structures
- Simplify the plugin launch code in `plugin/rpc/run_plugin.go`
- Reduce the surface area for RPC-related bugs

The migration happens **without coordination** — each plugin developer migrates independently, and the CLI team removes host-side code when usage drops below their threshold.

**Build-time dependency remains.** The runtime decoupling described above does not eliminate the plugin's compile-time dependency on the CLI's Go packages. Plugins still import `code.cloudfoundry.org/cli/plugin` (for the `CliConnection` interface and `plugin.Start()`) and `code.cloudfoundry.org/cli/plugin/models` (for the V2 model types the generated wrappers return). These are the *intended* public plugin contract — not internal packages — but they still couple the guest's build to the host's repository.

#### Achieving Full Build-Time Separation: Plugin SDK

> **TODO:** The following analysis describes the planned approach for full build-time decoupling. This will be implemented before the RFC approval process.

The build-time coupling can be eliminated by publishing a **plugin SDK** — a standalone Go module owned by the plugin side — containing copies of the host's plugin interface types:

- `plugin.Plugin` interface, `plugin.CliConnection` interface
- `plugin.Start()` function (the plugin-side RPC client setup)
- `plugin.PluginMetadata`, `plugin.Command`, `plugin.Usage`, `plugin.VersionType` types
- `plugin/models/*` — all V2 model types (`GetAppModel`, `GetAppsModel`, etc.)

**Why copying works:** The plugin and CLI are separate processes communicating via gob/net-rpc over TCP. The contract between them is the **wire format** — gob encodes structs by field name and type, not by Go package path. As long as both sides define identical struct layouts, gob serialization works correctly across the process boundary regardless of which Go module the types live in.

**Ownership model:** Today the CLI owns the interface definition and plugins are consumers. The SDK flips this — plugins own the interface, and the CLI implements the wire protocol against it. This is appropriate because the plugin interface is a *contract boundary*, not an internal implementation detail.

**Migration sequence:**

| Step | Who | What Changes |
|---|---|---|
| 1. Publish plugin SDK | Plugin side | SDK published with copies of current `plugin/` and `plugin/models/` types |
| 2. Plugins update imports | Plugin developers | Import paths change from `cli/plugin` to SDK — no logic changes |
| 3. `cf-plugin-migrate` generates SDK imports | Tool | Generated wrappers import SDK types instead of CLI types |
| 4. CLI keeps its own copy | CLI team | Nothing changes — no coordination needed |
| 5. CLI removes its copy (when ready) | CLI team | CLI imports the plugin SDK for the types it still needs (RPC server, gob encoding) |
| 6. V3 interface ships | Both | New V3 contract replaces V2 types; V2 SDK becomes legacy |

Steps 1–3 require no CLI team involvement. Steps 4–5 happen on the CLI team's timeline. The plugin side is fully decoupled after step 2.

**Risk:** If either side changes a struct field without the other matching, the gob wire format breaks. This risk is low — these types have been frozen for years, and the explicit plan is to not change them until the V3 plugin interface is defined.

**What this buys:** After step 2, plugins no longer import the CLI repository at all. The `go.mod` dependency on `code.cloudfoundry.org/cli` disappears. Plugins build faster, have fewer transitive dependencies, and are immune to CLI module restructuring.

### The Scanner: Automated V2 Usage Audit

#### What It Does

`cf-plugin-migrate scan` is an AST-based audit tool that analyzes a plugin's Go source code and produces a complete inventory of V2 interface usage. It answers the question every plugin developer and migration planner needs answered: **what exactly does this plugin depend on from the V2 interface?**

The scanner detects three categories of usage:

1. **V2 domain method calls** — `GetApp`, `GetApps`, `GetService`, `GetServices`, `GetOrg`, `GetOrgs`, `GetSpace`, `GetSpaces`, `GetOrgUsers`, `GetSpaceUsers`. For each call, it traces which fields of the returned model are actually accessed (e.g., only `Guid` and `Name` out of 20+ available fields on `GetAppModel`).

2. **`CliCommand` / `CliCommandWithoutTerminalOutput` calls** — Every call is detected with its command name and arguments. For `cf curl` calls, the scanner performs deep analysis: endpoint URL extraction, `json.Unmarshal` tracing, target struct type detection, field access tracking, and V2→V3 endpoint mapping.

3. **Session/context method calls** — `AccessToken`, `ApiEndpoint`, `GetCurrentOrg`, `GetCurrentSpace`, etc. These pass through unchanged and require no migration.

**Output:**
- **Human-readable summary** (stderr) — for quick review
- **YAML configuration** (stdout) — `cf-plugin-migrate.yml`, ready for the code generator

```bash
$ cf-plugin-migrate scan ./...
Found V2 domain method calls:

  commands/create-job.go:42  GetApp  → fields: Guid
  core/util.go:15            GetApps → fields: Guid, Name
    V3 API calls: Applications.Single, Applications.ListAll

Found CliCommand calls (legacy — not available in V3 plugin interface):

  client.go:92  CliCommandWithoutTerminalOutput("curl", "v2/apps")
    → V3 equivalent: /v3/apps (V2 entity/metadata envelope → V3 flat resources)
    → Unmarshalled into: apps (AppsModel)
    → Fields used: NextURL, Resources
```

#### How the YAML Maps V2 Fields to V3 API Calls

The key insight is that most plugins use only a fraction of the fields available on V2 model types. The YAML captures exactly which fields are used, and the generator maps each field to the minimum V3 API call required:

```yaml
# cf-plugin-migrate.yml — generated by scan, consumed by generate
schema_version: 1
package: main
methods:
  GetApp:
    fields: [Guid, Name]        # ← only 2 of 20+ fields used
  GetApps:
    fields: [Guid, Name, State] # ← only 3 of 8 fields used
```

Each field belongs to a **dependency group** — a set of V3 API calls. The generator includes a group only when at least one field from that group is requested:

| Fields Requested | V3 API Call | Call Count |
|---|---|---|
| `Guid`, `Name`, `State`, `SpaceGuid` | `Applications.Single()` | 1 |
| + `Routes` | + `Routes.ListForApp(include=domain)` | 2 |
| + `Command`, `Memory`, `InstanceCount` | + `Processes.ListForApp()` | 3 |
| + `RunningInstances` | + `Processes.GetStats()` | 4 |
| + `BuildpackUrl` | + `Droplets.GetCurrentForApp()` | 5 |

If a plugin only uses `Guid` and `Name`, the generated code makes **1 API call**. Compare this to the Rabobank library which always makes 10+ calls to populate every field regardless of usage.

#### Scanner Limitations (and Why They Don't Matter)

The scanner uses static AST analysis without full type resolution. This means there are patterns it cannot trace:

| Pattern | Example | Scanner Handles? |
|---|---|---|
| Direct field access | `app.Guid`, `app.Routes[0].Host` | **Yes** |
| Range iteration | `for _, s := range services { s.Name }` | **Yes** |
| Indexed access | `app.Routes[i].Domain.Name` | **Yes** |
| Passed to helper function | `helper(app)` then `app.Guid` in helper | **Flagged** — cross-function |
| Stored in struct field | `ctx.App = app` then `ctx.App.Guid` | **Flagged** — alias tracking |
| Reflection / interface cast | `reflect.ValueOf(app).FieldByName(...)` | **No** |

**Why the limitations don't matter:**

1. **Conservative is correct.** The scanner flags ambiguous cases for manual review rather than silently omitting them. A developer reviewing the YAML can add any missed fields in seconds.

2. **The common case is simple.** Analysis of 18 actively maintained plugins shows that the vast majority of field access happens directly at or near the call site. The OCF Scheduler, cf-targets-plugin, and Rabobank consumers all follow this pattern.

3. **The YAML is editable.** The scan output is a starting point, not a final answer. Developers can add fields, remove false positives, or adjust sub-field specifications before generating code.

4. **Reflection-based access doesn't exist in practice.** No surveyed plugin uses reflection to access V2 model fields.

### Migration Guide

#### Path A: Generated V2Compat Wrapper (Recommended)

This is the recommended approach for most plugins. The generated wrapper satisfies the `plugin.CliConnection` interface, so existing plugin code works without modification.

**Step 1: Scan**

```bash
cd your-plugin/
go run cf-plugin-migrate scan ./...  > cf-plugin-migrate.yml
```

Review the YAML output. The scanner auto-detects V2 domain method calls and the specific fields your plugin accesses.

**Step 2: Generate**

```bash
go run cf-plugin-migrate generate
```

This reads `cf-plugin-migrate.yml` and produces `v2compat_generated.go` — a single file containing:
- A `V2Compat` struct that embeds `plugin.CliConnection`
- A `NewV2Compat(conn)` constructor that builds a go-cfclient V3 client from the connection's credentials
- Reimplementations of only the V2 domain methods your plugin uses, making only the V3 API calls required for the fields you access
- Pass-through implementations for all other methods

**Step 3: Add the dependency** (domain method plugins only)

```bash
go get github.com/cloudfoundry/go-cfclient/v3
```

Session-only plugins (those that use only context methods) need no new dependency — the generated file contains only pass-through methods.

**Step 4: Drop in and build**

For **session-only plugins** — you're done. The generated file compiles alongside your existing code with zero changes. Example: cf-targets-plugin required zero code changes.

For **domain method plugins** — you have two options for wiring the wrapper:

**Option A: Shadow the connection parameter (one line)**

```go
func (p *MyPlugin) Run(cliConnection plugin.CliConnection, args []string) {
    cliConnection, err := NewV2Compat(cliConnection) // shadow the parameter
    if err != nil {
        fmt.Println(err)
        return
    }
    // All existing code works unchanged — cliConnection.GetApp() now uses V3
    app, err := cliConnection.GetApp("myapp")
    // ...
}
```

**Option B: Explicit variable (more visible)**

```go
func (p *MyPlugin) Run(conn plugin.CliConnection, args []string) {
    compat, err := NewV2Compat(conn)
    if err != nil {
        fmt.Println(err)
        return
    }
    // Pass compat wherever the plugin expects plugin.CliConnection
    app, err := compat.GetApp("myapp")
    // ...
}
```

Both options work because `*V2Compat` satisfies `plugin.CliConnection`. The plugin developer chooses based on their preference for explicitness.

**What the generated code looks like:**

For a plugin that uses only `GetApp` with fields `[Guid, Name]`:

```go
// GENERATED by cf-plugin-migrate — do not edit
func (c *V2Compat) GetApp(name string) (plugin_models.GetAppModel, error) {
    var model plugin_models.GetAppModel
    space, err := c.GetCurrentSpace()
    if err != nil {
        return model, err
    }
    app, err := c.cfClient.Applications.Single(context.Background(),
        &client.AppListOptions{
            Names:      client.Filter{Values: []string{name}},
            SpaceGUIDs: client.Filter{Values: []string{space.Guid}},
        })
    if err != nil {
        return model, err
    }
    model.Guid = app.GUID
    model.Name = app.Name
    return model, nil
}
```

One V3 API call. Two fields populated. All other fields remain zero-valued. The generated comment documents exactly which fields are populated and how many API calls are made.

#### Path B: Direct V3 Access (For New Development)

For plugins starting fresh or developers who want to use V3-native types directly, bypass the V2 compatibility wrapper entirely:

```go
func (p *MyPlugin) Run(conn plugin.CliConnection, args []string) {
    // Construct a go-cfclient V3 client from the connection's credentials
    endpoint, _ := conn.ApiEndpoint()
    token, _ := conn.AccessToken()
    skipSSL, _ := conn.IsSSLDisabled()

    cfg, _ := config.New(endpoint,
        config.Token(token),
        config.SkipTLSValidation(skipSSL),
    )
    cfClient, _ := client.New(cfg)

    // Use V3-native types — full model with metadata, lifecycle, relationships
    app, _ := cfClient.Applications.Single(context.Background(),
        &client.AppListOptions{
            Names: client.Filter{Values: []string{"my-app"}},
        })
    fmt.Printf("App: %s (%s)\n", app.Name, app.GUID)

    // V3 gives access to processes, sidecars, metadata — not available via V2
    processes, _, _ := cfClient.Processes.ListForApp(context.Background(), app.GUID, nil)
    for _, proc := range processes {
        fmt.Printf("  Process: %s, Instances: %d\n", proc.Type, proc.Instances)
    }
}
```

**When to choose Path B:**
- New plugin development
- Plugins that need V3-only concepts (multiple process types, sidecars, metadata labels)
- Developers comfortable rewriting call sites

**V2 to V3 method mapping** (for manual migration):

| V2 Method | V3 Replacement (go-cfclient) |
|---|---|
| `conn.GetApp(name)` | `cfClient.Applications.Single(ctx, &client.AppListOptions{Names: ..., SpaceGUIDs: ...})` |
| `conn.GetApps()` | `cfClient.Applications.ListAll(ctx, &client.AppListOptions{SpaceGUIDs: ...})` |
| `conn.GetService(name)` | `cfClient.ServiceInstances.Single(ctx, &client.ServiceInstanceListOptions{Names: ..., SpaceGUIDs: ...})` |
| `conn.GetServices()` | `cfClient.ServiceInstances.ListAll(ctx, &client.ServiceInstanceListOptions{SpaceGUIDs: ...})` |
| `conn.GetOrg(name)` | `cfClient.Organizations.Single(ctx, &client.OrganizationListOptions{Names: ...})` |
| `conn.GetOrgs()` | `cfClient.Organizations.ListAll(ctx, nil)` |
| `conn.GetSpaces()` | `cfClient.Spaces.ListAll(ctx, &client.SpaceListOptions{OrganizationGUIDs: ...})` |

Context methods (`AccessToken`, `ApiEndpoint`, `GetCurrentOrg`, `GetCurrentSpace`, `Username`, `IsLoggedIn`, `HasOrganization`, `HasSpace`, `IsSSLDisabled`, `HasAPIEndpoint`, `ApiVersion`) continue to use the existing `plugin.CliConnection` — these are the core contract methods that any future plugin interface preserves.

#### CliCommand / cf curl Migration

The scanner detects all `CliCommand` and `CliCommandWithoutTerminalOutput` calls and categorizes them:

**`cf curl` calls** — SHOULD migrate to go-cfclient or direct HTTP. The scanner provides V2→V3 endpoint mapping for 20 known V2 API paths:

```go
// Before:
output, _ := conn.CliCommandWithoutTerminalOutput("curl", "/v2/apps?q=name:myapp")
// Parse JSON from output[0]

// After:
apps, _ := cfClient.Applications.ListAll(ctx,
    &client.AppListOptions{Names: client.Filter{Values: []string{"myapp"}}})
```

**Workflow commands** (`push`, `bind-service`, `restage`, `delete`) — MAY continue to use `CliCommand` during the transition. These are multi-step workflow operations managed by the CLI. They can be replaced with go-cfclient calls as a future optimization, but they work correctly as-is and do not depend on V2 endpoints.

| CLI Command | Can migrate to go-cfclient? | Priority |
|---|---|---|
| `create-user-provided-service` | Yes: `cfClient.ServiceInstances.CreateUserProvided()` | Low — CLI handles it fine |
| `bind-service` | Yes: `cfClient.ServiceCredentialBindings.Create()` | Low |
| `unbind-service` | Yes: `cfClient.ServiceCredentialBindings.Delete()` | Low |
| `delete-service` | Yes: `cfClient.ServiceInstances.Delete()` | Low |

#### Worked Examples

##### cf-targets-plugin (Session-Only: Zero Code Changes)

The cf-targets-plugin uses only context methods (`GetCurrentOrg`, `GetCurrentSpace`, `HasOrganization`, `HasSpace`). No V2 domain methods, no `CliCommand` calls.

**Migration:** Run `cf-plugin-migrate scan` → generates a session-only V2Compat with pure pass-through methods. Drop in the file, build, install. **Zero lines of plugin code changed.**

##### OCF Scheduler (Domain Methods: One Dependency + One Line)

The [OCF Scheduler plugin](https://github.com/cloudfoundry-community/ocf-scheduler-cf-plugin) uses `GetApp` (for `.Guid` only) and `GetApps` (for `.Guid`, `.Name`). No other fields are accessed — not `State`, `Routes`, `Memory`, `Instances`, or any other model attribute. The plugin uses V2 model methods purely as a name/GUID mapping layer.

**Scan output:**

```yaml
schema_version: 1
package: main
methods:
  GetApp:
    fields: [Guid]
  GetApps:
    fields: [Guid, Name]
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

**Migration:**
1. `go get github.com/cloudfoundry/go-cfclient/v3`
2. Drop in generated `v2compat_generated.go`
3. Add one line: `cliConnection, err := NewV2Compat(cliConnection)`

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

**Validated:** Built, installed via `cf install-plugin`, ran `cf create-job` against live CAPI V3 v3.180.0 — `GetApp` correctly resolved app name to GUID via V3.

##### metric-registrar (Complex: Domain Methods + Curl Calls)

The [metric-registrar plugin](https://github.com/pivotal-cf/metric-registrar-cli) is the most V2-coupled active plugin in the survey. It uses three V2 domain methods **and** four V2 CAPI curl calls. This analysis covers the migration in two phases: domain methods first, then curl calls.

**Phase 1: Domain Methods**

| V2 Method | Call Sites | Fields Accessed | V3 API Calls |
|---|---|---|---|
| `GetApp(name)` | `register.go` (1), `unregister.go` (2) | `Guid`, `Name`, `Routes[].Host`, `Routes[].Domain.Name`, `Routes[].Port`, `Routes[].Path` | 2 (app + routes) |
| `GetApps()` | `list.go` (2 — log formats + metrics endpoints) | `Guid`, `Name` | 1 |
| `GetServices()` | `register.go` (1) | `Name` only | 1 (server-side filtered) |

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

**Phase 2: Replacing V2 `cf curl` Calls**

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

#### Deep Analysis: V2 Ports → V3 Route Destinations

The V2 `ports` array on the app entity (`PUT /v2/apps/{guid} {"ports": [8080, 9090]}`) has **no direct equivalent in V3**. This is a fundamental architectural change, not a field rename:

| Concept | V2 | V3 |
|---|---|---|
| Declare app can serve on a port | `PUT /v2/apps/{guid}` with `ports` array | No equivalent — not needed |
| Route traffic to a specific port | Route mapping with `app_port` | Route destination with `port` field |
| Default port | 8080 (buildpack) or Docker EXPOSE | Same defaults, but declared per-destination |
| Read which ports are mapped | `GET /v2/route_mappings` | `GET /v3/routes/{route_guid}/destinations` |
| Atomic read-modify-write | Single PUT on app entity | Per-route: discover routes → manage destinations per route |

**What metric-registrar actually does with ports:**

The `--internal-port` flag does **not** create CF internal routes (`apps.internal` domain). "Internal" here means "a port internal to the container" — the platform-side metrics scraper reaches the app on that port directly via the container overlay network IP, bypassing the gorouter entirely.

The port lifecycle:
1. **Register:** Read current `entity.ports` (e.g., `[8080]`), append the metrics port (e.g., `[8080, 2112]`), write back via `PUT /v2/apps/{guid}`
2. **Unregister:** Read current ports, compute which ports are still needed by remaining registrations, write reduced array

The V2 `ports` array served a dual purpose: it told Diego which ports to expose on the container *and* which ports could be targeted by route mappings. In V3, these concerns are split:

- **Routable ports** are managed via route destinations (`POST /v3/routes/{guid}/destinations` with `port` field). go-cfclient supports this fully: `InsertDestinations()`, `ReplaceDestinations()`, `RemoveDestination()`.
- **Non-routable container ports** (what the scraper needs) have no V3 API equivalent. The V2 `ports` array was the only way to tell Diego "expose port 2112 on the container network without routing it through gorouter."

**Migration options for non-routable ports:**

1. **Internal route + destination:** Create a route on an internal domain (`apps.internal`), then add a destination targeting the metrics port. The scraper would reach the app via `app-name.apps.internal:2112`. This works but changes the scraper's connectivity model — it must resolve the internal route instead of using the container IP directly.

2. **Process health-check port:** If the metrics port is also the health-check port, the process configuration already exposes it. But metric-registrar uses a dedicated metrics port separate from the app's health check.

3. **Sidecar process:** Declare the metrics endpoint as a sidecar process in the app manifest. This is architecturally clean but requires manifest changes rather than API calls.

4. **Keep using `cf curl`:** Continue calling the V2 endpoints for port management until they are removed. This buys time but doesn't solve the problem.

**Recommendation for metric-registrar:** Option 1 (internal route + destination) is the most viable V3 migration path. It requires changing the scraper's connectivity model from "container IP + port" to "internal route hostname + port," which is a platform-side change — not just a plugin change. This confirms that the port migration is a **cross-component redesign** requiring coordination between the plugin and the metric-registrar platform component.

**Impact on the transitional migration approach:** The port migration cannot be handled by the V2Compat wrapper or the code generator. It is explicitly a "category 2: redesign" migration (see below) that requires human judgment and cross-component coordination. The generated wrapper approach correctly surfaces this as work the developer must handle manually.

**Phase 2 result:**

| Component | Migration Type | Effort |
|---|---|---|
| `registrations/fetcher.go` — UPS listing | Clean V3 substitution | Low — swap URL patterns and JSON paths |
| `registrations/fetcher.go` — binding lookup | Clean V3 substitution | Low — `relationships.app.data.guid` |
| `ports/ports.go` — read ports | Structural redesign | **High** — flat array → per-route destinations |
| `ports/ports.go` — write ports | Structural redesign | **High** — app-centric → route-centric |
| CLI command delegation | Optional future work | Low — works as-is |

**Key insight:** The metric-registrar migration reveals two distinct categories of V2→V3 work:

1. **Substitution** — Same concept, different API shape. Domain methods (`GetApp`, `GetApps`, `GetServices`) and flat V2 endpoints (UPS listing, binding lookup) map to V3 equivalents with minor field-path changes. The generated wrapper approach handles this well.

2. **Redesign** — The V3 model is fundamentally different. App ports moving to route destinations is not a field rename — it's a different domain model. No generator can bridge this; the plugin developer must understand the V3 model and rewrite the affected code.

The transitional approach handles category 1 automatically and surfaces category 2 as the work that requires human judgment.

##### mysql-cli-plugin (Heavy CliCommand Usage)

The mysql-cli-plugin demonstrates the scanner's value for `CliCommand`-heavy plugins. The scanner detected 14 `CliCommand` calls:

- `bind-service`, `create-service`, `delete`, `push`, `start`, `logs`, `rename-service`, `service-key`, `create-service-key`, `delete-service-key`, 2× `curl`, plus 1 dynamic command via variable
- Plus `GetService` domain method (fields: `LastOperation.State`, `LastOperation.Description`)

The scan output gives the developer a complete inventory of what needs attention, categorized by urgency: domain methods (must migrate), curl calls (should migrate), workflow commands (can stay).

### Companion Package Design

The companion package at `code.cloudfoundry.org/cli-plugin-helpers/cfclient` SHOULD be published to support the transitional migration:

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

Note: Unlike the Rabobank library, this companion package does **not** reimplement V2 domain methods via V3. Plugins SHOULD either use go-cfclient directly for domain operations, or use the generated V2 compatibility wrappers that populate only the fields a plugin declares it needs.

### Lessons from the Rabobank Implementation

The Rabobank `cf-plugins` library validates this approach but also reveals pitfalls to avoid.

#### What Worked

- **Zero host changes.** The library wraps `plugin.CliConnection` and calls `plugin.Start()` — compatible with any CF CLI version.
- **Incremental adoption.** Consumer plugins adopt at their own pace — 2 of 4 Rabobank consumers use the library.
- **Two migration tiers.** `plugins.Start()` provides transparent V3 reimplementation; `plugins.Execute()` provides direct `CfClient()` access.

#### Consumer Plugin Analysis: Was the Full Reimplementation Necessary?

The `cf-plugins` library reimplements **10 V2 domain methods** via V3. To evaluate whether this scope was justified, we analyzed both the current state and the git history of all 4 consumer plugins.

**Historical V2 Domain Method Usage:**

| Plugin | V2 Methods Used Historically | How They Migrated | Key Commits |
|---|---|---|---|
| scheduler-plugin | `GetServices()`, `GetService()`, `GetApp()` | Migrated directly to go-cfclient/v3 — did **not** adopt cf-plugins | [`57130bdb`](https://github.com/rabobank/scheduler-plugin/commit/57130bdb), [`e682b800`](https://github.com/rabobank/scheduler-plugin/commit/e682b800) |
| credhub-plugin | `GetService()` | Adopted cf-plugins — **2-line change** in `main.go` | [`e5355478`](https://github.com/rabobank/credhub-plugin/commit/e5355478), [`7cdaded9`](https://github.com/rabobank/credhub-plugin/commit/7cdaded9) |
| npsb-plugin | None (context methods only, from day one) | No migration needed | [`ef0c3e10`](https://github.com/rabobank/npsb-plugin/commit/ef0c3e10) |
| idb-plugin | None (built with cf-plugins from the start, Oct 2025) | Born V3-native via `CfClient()` | [`938005ae`](https://github.com/rabobank/idb-plugin/commit/938005ae) |

The cf-plugins library was created **Oct 2, 2025** ([`a0486ef6`](https://github.com/rabobank/cf-plugins/commit/a0486ef6)). The scheduler-plugin's migration commit message is explicit: *"remove dependency on some cliConnection calls since they still require cf v2 api"*. Commit authorship confirms the same developer migrated the scheduler-plugin and then created the cf-plugins library, generalizing the pattern for other consumers.

**The library solved a real problem.** The credhub-plugin migration demonstrates the library's value: a 2-line change (`plugin.Start()` → `plugins.Start()`) transparently replaced V2 RPC-backed domain method calls with V3 API calls.

**But the scope was broader than needed.** Across all 4 consumer plugins, historically and currently, only **3 of 10** reimplemented methods were ever called:

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

The library reimplemented 10 methods, but only 3 were ever used — and those 3 accessed a combined total of **5 unique fields**. The remaining 7 reimplementations were speculative.

**Current State (Post-Migration):**

| Plugin | Uses cf-plugins? | V2 Domain Methods Called Now | Fields Accessed Now |
|---|---|---|---|
| scheduler-plugin | **No** | None | N/A — uses `go-cfclient/v3` directly |
| npsb-plugin | **No** | None | N/A — uses direct HTTP with `AccessToken()` |
| idb-plugin | Yes (`Execute()`) | None | N/A — uses `CfClient()` for V3 access |
| credhub-plugin | Yes (`Start()`) | **`GetService()` only** | `.Guid`, `.ServiceOffering.Name`, `.LastOperation.State` |

Today, only 1 method is called by 1 plugin, accessing 3 fields.

**Takeaway:** The cf-plugins library was a valid response to a real migration pressure, and the `plugins.Start()` pattern (transparent drop-in) was an elegant design. But the all-or-nothing reimplementation strategy — building every V2 method before knowing which ones consumers need — is exactly the waste the generated wrapper approach avoids. Build only what you use.

#### What the Generated Approach Improves

| Metric | Rabobank Library | Generated Wrapper |
|---|---|---|
| Methods reimplemented | 10 (all) | Only those declared in YAML |
| API calls for `GetApp` | 10+ (all fields) | 1–10 (only declared field groups) |
| API calls for `GetService` | 3 (instance, plan, offering) | **1** (using `fields` parameter) |
| API calls for `GetServices` (N instances) | 1 + 3×N | **2** (using `fields` + `include`) |
| Lines of wrapper code | ~500 | ~40–200 (varies by config) |

The most significant optimization is for service-related methods: the generator uses CAPI V3 `fields[service_plan]` and `fields[service_plan.service_offering]` parameters to retrieve instance, plan, and offering data in a **single API call**, eliminating the per-instance GET requests that Rabobank makes.

#### Rabobank Caveats in Context

The Rabobank README lists several caveats. Some are **not actually limitations** — they faithfully represent what V2 always provided:

| Rabobank Caveat | Actually a problem? | Explanation |
|---|---|---|
| Single buildpack only | **No.** | V2's `BuildpackUrl` was always a single string. The wrapper correctly returns the first buildpack. |
| Single process type | **No.** | V2 had no concept of multiple process types. The wrapper populates from the `web` process, matching V2 behavior. |
| `IsAdmin` always false | **Avoidable.** | Rabobank skipped the UAA role query. The generated wrapper includes it only if declared. |
| No per-app stats in list | **Avoidable.** | Rabobank omitted stats to avoid N+1 per-process calls. The generated wrapper includes stats calls only if the plugin declares it needs `RunningInstances`. |
| 11 API calls for `GetApp()` | **Avoidable.** | Rabobank populates every field. The generated wrapper makes only the calls needed for declared fields. |

#### Implementation Bugs to Avoid

1. **Do not strip the token prefix manually.** Rabobank uses `token[7:]` to strip `"bearer "`. go-cfclient's `config.Token()` handles this internally.
2. **Do not hardcode SSL settings.** Pass through the host's `IsSSLDisabled()` value.
3. **Do not hardcode user agent strings.** Derive from the plugin's metadata.

### go-cfclient V3 Guidance

#### Library Status

go-cfclient v3 is published at `github.com/cloudfoundry/go-cfclient/v3`. As of March 2026, the latest release is **v3.0.0-alpha.20**. Despite the alpha label, the library is in production use by multiple CF CLI plugins and has near-complete CAPI V3 coverage.

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

Plugins SHOULD use **v3.0.0-alpha.17 or later**. This version:
- Includes `config.Token()` that handles the `"bearer "` prefix internally
- Supports `config.SkipTLSValidation()` with a boolean parameter
- Has stable interfaces for all resources used by the generated wrappers

Plugins SHOULD pin to a specific alpha version in `go.mod` and upgrade deliberately. Once go-cfclient releases v3.0.0 stable, all plugins SHOULD upgrade to it.

#### CAPI V3 Coverage

go-cfclient v3 implements **31 of 35 CAPI V3 resource groups with full coverage**. Every resource needed for plugin migration is fully supported. The library adds value beyond raw coverage:
- **Pagination:** `ListAll` (auto-pages), `First`, `Single` helpers
- **Include-based eager loading:** e.g., list apps with their spaces and orgs in one call
- **Async job polling:** `PollComplete`, `PollStaged`, `PollReady`
- **Typed filters:** `AppListOptions`, `ServiceInstanceListOptions`, etc.

#### CF API Version Floor

go-cfclient v3 requires CAPI V3 endpoints. The minimum depends on which resources the plugin uses:

| Resource | Minimum CAPI Version | CF Deployment |
|---|---|---|
| Apps, Spaces, Orgs (core) | 3.0.0 | 1.0+ |
| Service Instances (user-provided) | 3.0.0 | 1.0+ |
| Service Credential Bindings | 3.77.0 | ~18.0+ |
| Route Destinations | 3.77.0 | ~18.0+ |
| Service Plans, Offerings | 3.77.0 | ~18.0+ |

Most actively maintained CF foundations run CAPI 3.100+ (CF Deployment 25+), so the version floor is not a practical concern. The generated wrappers SHOULD document the minimum CAPI version per V3 resource used.

#### Token Lifecycle

`AccessToken()` returns a *snapshot* — the token value at the moment the RPC call is made. The host manages token refresh internally (via `config.ConfigRepository`), but go-cfclient does not receive a refresh token. This creates two patterns depending on plugin lifetime:

**Short-lived plugins** (most plugins — run a command and exit):

Use `config.Token()` directly. The token returned by `AccessToken()` at startup is valid for the plugin's entire lifetime (CF tokens typically last 10–20 minutes; most plugin commands complete in seconds).

```go
cfg, _ := config.New(endpoint,
    config.Token(token),           // snapshot token — valid for short operations
    config.SkipTLSValidation(skipSSL),
)
```

**Long-running plugins** (polling loops, watch commands, streaming):

Use `config.TokenProvider()` to re-fetch from the host's RPC on each API call. The host refreshes the token transparently via its own OAuth2 flow, so each call to `AccessToken()` returns a current token.

```go
cfg, _ := config.New(endpoint,
    config.TokenProvider(func() (string, error) {
        return conn.AccessToken() // re-fetches from host RPC each time
    }),
    config.SkipTLSValidation(skipSSL),
)
```

**When `TokenProvider` is necessary:** If a plugin makes API calls over a span longer than the token's TTL (typically 10–20 minutes), a snapshot token will expire mid-operation. Symptoms: go-cfclient returns 401 errors after the plugin has been running for several minutes. The fix is switching from `config.Token()` to `config.TokenProvider()`.

**Edge case:** If the host (CF CLI) exits while the plugin is still running, `conn.AccessToken()` will return an RPC error. Long-running plugins SHOULD handle this gracefully — see Error Handling at the Boundary.

The generated V2Compat wrapper uses `config.Token()` by default (appropriate for the common case). Plugins that need long-running behavior SHOULD switch to `config.TokenProvider()` manually — the generator does not make this choice automatically because it cannot determine plugin lifetime from static analysis.

### UAA and CredHub

The CAPI V2→V3 transition does not affect UAA or CredHub interfaces. Only 1 of 18 surveyed plugins calls UAA directly (a `client_credentials` token exchange). Only 1 plugin talks to CredHub (via a CAPI-resolved broker URL). The core contract's `AccessToken()` covers plugin needs.

### Error Handling at the Boundary

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

### Build System Integration

- **`//go:generate` directive** for regeneration: `//go:generate cf-plugin-migrate generate`
- **Makefile target** (e.g., `make generate`) for Make-based builds
- **Generated file** SHOULD be checked into version control so `go install` works without the generator tool
- **Dependency management.** Adding go-cfclient/v3 pulls in `golang.org/x/oauth2`, `google.golang.org/protobuf`, and several transitive dependencies. For plugins that vendor dependencies, this increases the vendor directory. For plugins using module proxies, the impact is minimal.

### Test Migration

- **Existing mocks survive.** Tests that mock `plugin.CliConnection` continue to work for context methods (`AccessToken`, `GetCurrentSpace`, etc.).
- **New domain tests mock go-cfclient.** Plugins that switch to go-cfclient V3 for domain operations need either a `*httptest.Server` or a mock client. go-cfclient provides `fake.Client` in its test package.
- **Generated wrapper tests.** The generator SHOULD produce a companion `_test.go` file with table-driven tests that verify correct field population against a test server.

### Proof-of-Concept Candidates

To validate the transitional approach, three plugins from the [plugin survey](plugin-survey.md) represent increasing levels of migration complexity:

#### Tier 1: Simple — list-services

- **V2 methods used:** `GetApp()` — for GUID resolution only
- **Fields consumed:** `Guid` only (1 field → 1 V3 API call)
- **Migration:** Replace `conn.GetApp(name)` with `cfClient.Applications.Single(ctx, opts)`, read `.GUID` — or generate a wrapper that returns `GetAppModel{Guid: app.GUID}`
- **Why:** Simplest possible case. Validates the end-to-end flow with minimal risk.

#### Tier 2: Moderate — OCF Scheduler

- **V2 methods used:** `GetApp()`, `GetApps()`
- **Fields consumed:** `Name`, `Guid`, `State` (3 fields → 1 V3 API call each)
- **Migration:** Two methods to replace, both using only core app fields available from a single `Applications` call
- **Why:** Actively maintained, representative of the common pattern. Already uses direct HTTP for scheduler operations — only the app lookup needs migration.

#### Tier 3: Complex — metric-registrar

- **V2 methods used:** `GetApp()`, `GetApps()`, `GetServices()`
- **Additional V2 dependency:** `cf curl /v2/user_provided_service_instances`, `/v2/apps/{guid}`
- **Fields consumed:** Multiple fields across app and service models
- **Migration:** Requires both generated wrappers (for V2 model methods) and `cf curl` replacement (for V2 CAPI endpoints). Tests the full migration path.
- **Why:** Most V2-coupled active plugin. If the transitional approach works here, it works everywhere.

#### Secondary Candidates

| Plugin | V2 Methods | Notes |
|---|---|---|
| spring-cloud-services | `GetService()`, `GetApps()` | Clean architecture, good test of service model migration |
| stack-auditor | `GetOrgs()` + `cf curl /v2/...` | Tests migration of both model methods and V2 curl calls |
| service-instance-logs | `GetService()` | V2 chain traversal (service → plan → service offering) — complex V3 mapping |

### Future Work: CAPI OpenAPI and Polyglot Clients

CAPI V3 has **no official OpenAPI specification**. All existing client libraries (Go, Java, Python) are hand-written. The community [capi-openapi-spec](https://github.com/cloudfoundry-community/capi-openapi-spec) project is the most promising path toward a machine-readable spec, which would enable auto-generated clients in any language.

Available CAPI V3 client libraries:

| Library | Language | Status |
|---|---|---|
| [go-cfclient](https://github.com/cloudfoundry/go-cfclient) | Go | Official, recommended |
| [cf-java-client](https://github.com/cloudfoundry/cf-java-client) | Java | Official |
| [cf-python-client](https://github.com/cloudfoundry-community/cf-python-client) | Python | Community |

## Technical Reference

This section provides the detailed data tables used by the `cf-plugin-migrate` tool. It is primarily for tool developers and contributors.

### YAML Schema: `cf-plugin-migrate.yml`

The YAML configuration declares which V2 domain methods the plugin calls and which fields it accesses. The generator uses this to produce minimal wrappers with only the V3 API calls required.

**Formal schema (version 1):**

```yaml
# Required: schema version for forward compatibility
schema_version: 1

# Go package name for the generated file (default: "main")
package: main

# V2 domain methods used by the plugin.
# Keys are method names from plugin.CliConnection.
# Only methods that return plugin_models.* types are supported.
methods:
  GetApp:
    # Top-level fields accessed on GetAppModel
    fields: [Guid, Name, State, Routes, Stack]
    # Sub-fields for composite types (required when parent is listed)
    # Convention: lowercase parent field name + "_fields"
    route_fields: [Host, Domain.Name, Port, Path]
    stack_fields: [Name, Guid, Description]
  GetApps:
    fields: [Guid, Name, State]
  GetServices:
    fields: [Guid, Name, ServicePlan, IsUserProvided]
    service_plan_fields: [Name, Guid]
  GetService:
    fields: [Guid, Name, DashboardUrl, ServicePlan, ServiceOffering, LastOperation]
    service_plan_fields: [Name, Guid]
    service_offering_fields: [Name, DocumentationUrl]
    last_operation_fields: [Type, State, Description]
  GetOrg:
    fields: [Guid, Name, Spaces, Domains, QuotaDefinition]
  GetOrgs:
    fields: [Guid, Name]
  GetSpace:
    fields: [Guid, Name, Organization, Applications, ServiceInstances]
  GetSpaces:
    fields: [Guid, Name]
  GetOrgUsers:
    fields: [Guid, Username, Roles]
  GetSpaceUsers:
    fields: [Guid, Username, Roles]

# CliCommand calls detected by scanner (informational — not consumed by generator)
cli_commands:
  - file: main.go
    line: 92
    method: CliCommandWithoutTerminalOutput
    command: curl
    endpoint: v2/apps
    v3_endpoint: /v3/apps
```

**Schema rules:**

| Rule | Description |
|---|---|
| `schema_version` | Required. Currently `1`. Allows the tool to evolve the schema without breaking existing configs. |
| `package` | Go package name for the generated `v2compat_generated.go`. Defaults to `main`. |
| `methods` | Map of V2 method name → field specification. Only methods listed are generated. |
| `fields` | List of top-level field names from the corresponding `plugin_models.*` struct. Must match the Go field name exactly. |
| `*_fields` | Sub-field specifiers for composite fields. Named `{lowercase_parent}_fields`. Dot notation for nested access (e.g., `Domain.Name`). Only required when the parent field is listed in `fields`. |
| `cli_commands` | Scanner output for CliCommand calls. Informational — the generator does not consume this section. |

**Supported methods and sub-field keys:**

| Method | Return Type | Available Sub-field Keys |
|---|---|---|
| `GetApp` | `GetAppModel` | `route_fields`, `stack_fields`, `instance_fields`, `service_fields` |
| `GetApps` | `[]GetAppsModel` | `route_fields` |
| `GetService` | `GetService_Model` | `service_plan_fields`, `service_offering_fields`, `last_operation_fields` |
| `GetServices` | `[]GetServices_Model` | `service_plan_fields`, `service_fields`, `last_operation_fields` |
| `GetOrg` | `GetOrg_Model` | `space_fields`, `domain_fields`, `space_quota_fields`, `quota_fields` |
| `GetOrgs` | `[]GetOrgs_Model` | (none) |
| `GetSpace` | `GetSpace_Model` | `org_fields`, `app_fields`, `service_instance_fields`, `domain_fields`, `security_group_fields`, `space_quota_fields` |
| `GetSpaces` | `[]GetSpaces_Model` | (none) |
| `GetOrgUsers` | `[]GetOrgUsers_Model` | (none — flat struct) |
| `GetSpaceUsers` | `[]GetSpaceUsers_Model` | (none — flat struct) |

### V3 API `include` and `fields` Parameter Availability

The CAPI V3 API supports two mechanisms for retrieving related resources in a single call:

- **`include`** — returns full related resources in an `included` section of the response
- **`fields`** — returns selected fields of related resources (supports nested paths like `service_plan.service_offering`)

| Endpoint | `include` Values | `fields` Resources |
|---|---|---|
| `/v3/apps` | `space`, `org`, `space.organization` | — |
| `/v3/routes` | `domain`, `space`, `space.organization` | — |
| `/v3/spaces` | `org`, `organization` | — |
| `/v3/roles` | `user`, `organization`, `space` | — |
| `/v3/service_credential_bindings` | `app`, `service_instance` | — |
| `/v3/service_instances` | — | `service_plan`, `service_plan.service_offering`, `service_plan.service_offering.service_broker` |
| `/v3/service_plans` | `service_offering` | `service_offering.service_broker` |
| `/v3/service_offerings` | — | `service_broker` |

The generator uses `include` and `fields` where available to minimize API calls.

### Generator Optimization Summary

The `include` and `fields` parameters available in CAPI V3 significantly reduce API calls compared to Rabobank's approach:

| Method | Rabobank Calls | Generator Calls (all fields) | Key Optimization |
|---|---|---|---|
| `GetApp` | 10 | 10 | `include=domain` on routes (eliminates URL parsing) |
| `GetApps` | 1 (partial) | 1 + per-app | Per-app calls for process/stats fields; results scoped by user permissions |
| `GetService` | 3 | **1** | `fields[service_plan]` + `fields[service_plan.service_offering]` |
| `GetServices` | 1 + 3×N | **2** | `fields` on list + single bindings call with `include=app` |
| `GetOrg` | 5 | 5 | No `include`/`fields` available on these endpoints |
| `GetSpace` | 7 | **6** | `include=organization` on spaces (eliminates org GET) |
| `GetOrgUsers` | 2 | 2 | Both use `include=user` |
| `GetSpaceUsers` | 3 | 3 | Both use `include=user` |

The most dramatic improvement is for service-related methods: `GetService` drops from 3 calls to 1, and `GetServices` drops from 1 + 3×N calls to 2 calls regardless of instance count.

### Complete V2→V3 Field Mapping Reference

Fields are organized into **dependency groups** — sets of fields that share the same V3 API call. The generator adds a V3 call only when at least one field from that group is requested.

The mapping is derived from first-principles analysis of the [V2 plugin model types](https://github.com/cloudfoundry/cli/tree/main/plugin/models) and the [CAPI V3 API documentation](https://v3-apidocs.cloudfoundry.org), validated against the [Rabobank implementation](https://github.com/rabobank/cf-plugins/blob/main/connection.go) and tested against a live CAPI V3 endpoint (v3.180.0).

#### GetAppModel — `GetApp(appName string)`

The most complex model. Rabobank populates all fields with 10 V3 API calls. The generated approach selects only the groups needed.

| Group | V3 API Call(s) | V2 Fields | V3 Field Path | Notes |
|---|---|---|---|---|
| **1: App** | `Applications.Single(name, spaceGUID)` | `Guid` | `.GUID` | Always required — base call |
| | | `Name` | `.Name` | |
| | | `State` | `.State` | |
| | | `SpaceGuid` | (input parameter) | Passed as filter, not from response |
| **2: Process** | `Processes.ListForApp(appGUID)` + `Processes.Get(processGUID)` | `Command` | `.Command` | First process type only (V2 had single process) |
| | | `DetectedStartCommand` | `.Command` | V3 does not distinguish detected vs explicit |
| | | `DiskQuota` | `.DiskInMB` | |
| | | `InstanceCount` | `.Instances` | |
| | | `Memory` | `.MemoryInMB` | |
| | | `HealthCheckTimeout` | `.HealthCheck.Data.Timeout` | Pointer — nil means no timeout set |
| **3: Stats** | `Processes.GetStats(processGUID)` | `RunningInstances` | `len(.Stats)` | Requires Group 2 for process GUID |
| | | `Instances[].State` | `.Stats[].State` | |
| | | `Instances[].Details` | `.Stats[].Details` | Pointer |
| | | `Instances[].Since` | computed from `.Stats[].Uptime` | `time.Now() - uptime` |
| | | `Instances[].CpuUsage` | `.Stats[].Usage.CPU` | |
| | | `Instances[].DiskQuota` | `.Stats[].DiskQuota` | In bytes (not MB) |
| | | `Instances[].DiskUsage` | `.Stats[].Usage.Disk` | |
| | | `Instances[].MemQuota` | `.Stats[].MemoryQuota` | |
| | | `Instances[].MemUsage` | `.Stats[].Usage.Memory` | |
| **4: Droplet** | `Droplets.GetCurrentForApp(appGUID)` | `BuildpackUrl` | `.Buildpacks[0].Name` | V2 exposed single buildpack; V3 supports multiple |
| | | `PackageState` | `.State` | Droplet state, not package state |
| | | `StagingFailedReason` | `.Error` | Pointer |
| **5: Stack** | `Stacks.Single(droplet.Stack)` | `Stack.Guid` | `.GUID` | Requires Group 4 for stack name from droplet |
| | | `Stack.Name` | `.Name` | |
| | | `Stack.Description` | `.Description` | Pointer |
| **6: Package** | `Packages.ListForAppAll(appGUID)` | `PackageUpdatedAt` | last `.UpdatedAt` | Assumes latest package is last in list |
| **7: Env** | `Applications.GetEnvironmentVariables(appGUID)` | `EnvironmentVars` | (full map) | |
| **8: Routes** | `Routes.ListForApp(appGUID, include=domain)` | `Routes[].Guid` | `.GUID` | `include=domain` eliminates separate domain lookups |
| | | `Routes[].Host` | `.Host` | |
| | | `Routes[].Path` | `.Path` | |
| | | `Routes[].Port` | `.Port` | Pointer |
| | | `Routes[].Domain.Guid` | `.Relationships.Domain.Data.GUID` | |
| | | `Routes[].Domain.Name` | (included Domain).Name | Via `include=domain` |
| **9: Services** | `ServiceCredentialBindings.List(appGUID, include=service_instance)` | `Services[].Guid` | (included SI).GUID | `include=service_instance` avoids N+1 |
| | | `Services[].Name` | (included SI).Name | |

**Dependency chain:** Group 3 requires Group 2 (process GUID). Group 5 requires Group 4 (stack name from droplet). All other groups are independent.

#### GetAppsModel — `GetApps()`

| Group | V3 API Call(s) | V2 Fields | V3 Field Path | Notes |
|---|---|---|---|---|
| **1: Apps** | `Applications.ListAll(spaceGUID)` | `Guid` | `.GUID` | Always required |
| | | `Name` | `.Name` | |
| | | `State` | `.State` | |
| **2: Process** | `Processes.ListForApp()` **per app** | `TotalInstances` | `.Instances` | Per-app call |
| | | `Memory` | `.MemoryInMB` | |
| | | `DiskQuota` | `.DiskInMB` | |
| **3: Stats** | `Processes.GetStats()` **per app** | `RunningInstances` | `len(.Stats)` | Requires Group 2 |
| **4: Routes** | `Routes.ListForApp(include=domain)` **per app** | `Routes[].*` | (see GetAppModel Group 8) | Per-app call |

#### GetService_Model — `GetService(serviceName string)`

| Group | V3 API Call(s) | V2 Fields | V3 Field Path | Notes |
|---|---|---|---|---|
| **1: Instance+Plan+Offering** | `ServiceInstances.Single(name, spaceGUID)` with `fields[service_plan]` and `fields[service_plan.service_offering]` | `Guid` | `.GUID` | **Single call** — `fields` parameters include plan and offering |
| | | `Name` | `.Name` | |
| | | `DashboardUrl` | `.DashboardURL` | Pointer |
| | | `IsUserProvided` | `.Type == "user-provided"` | |
| | | `LastOperation.*` | `.LastOperation.*` | |
| | | `ServicePlan.Name` | (included plan).Name | Via `fields[service_plan]` |
| | | `ServicePlan.Guid` | (included plan).GUID | |
| | | `ServiceOffering.Name` | (included offering).Name | Via `fields[service_plan.service_offering]` |
| | | `ServiceOffering.DocumentationUrl` | (included offering).DocumentationURL | |

**Optimization:** Retrieves instance, plan, and offering in **1 API call** vs. Rabobank's 3.

#### GetServices_Model — `GetServices()`

| Group | V3 API Call(s) | V2 Fields | V3 Field Path | Notes |
|---|---|---|---|---|
| **1: Instances+Plans+Offerings** | `ServiceInstances.ListAll(spaceGUID)` with `fields[service_plan]` and `fields[service_plan.service_offering]` | `Guid`, `Name`, `IsUserProvided`, `LastOperation.*`, `ServicePlan.*`, `Service.Name` | Various | **Single call** with `fields` |
| **2: Apps** | `ServiceCredentialBindings.ListAll(include=app)` | `ApplicationNames` | (included App).Name | Single call with `include=app` |

**Optimization:** 2 calls regardless of instance count vs. Rabobank's 1 + 3×N.

#### GetOrg_Model — `GetOrg(orgName string)`

| Group | V3 API Call(s) | V2 Fields |
|---|---|---|
| **1: Org** | `Organizations.Single(name)` | `Guid`, `Name` |
| **2: Quota** | `OrganizationQuotas.Get(quotaGUID)` | `QuotaDefinition.*` |
| **3: Spaces** | `Spaces.ListAll(orgGUID)` | `Spaces[].Guid`, `Spaces[].Name` |
| **4: Domains** | `Domains.ListForOrganization(orgGUID)` | `Domains[].Guid`, `Domains[].Name`, `Domains[].OwningOrganizationGuid`, `Domains[].Shared` |
| **5: SpaceQuotas** | `SpaceQuotas.ListAll(orgGUID)` | `SpaceQuotas[].Guid`, `SpaceQuotas[].Name`, ... |

#### GetSpace_Model — `GetSpace(spaceName string)`

| Group | V3 API Call(s) | V2 Fields | Notes |
|---|---|---|---|
| **1: Space+Org** | `Spaces.Single(name, include=organization)` | `Guid`, `Name`, `Organization.*` | `include=organization` eliminates separate org GET |
| **2: Apps** | `Applications.ListAll(spaceGUID)` | `Applications[].Guid`, `Applications[].Name` | |
| **3: Services** | `ServiceInstances.ListAll(spaceGUID)` | `ServiceInstances[].Guid`, `ServiceInstances[].Name` | |
| **4: Domains** | `Domains.ListAll(orgGUID)` | `Domains[].*` | Org GUID from Group 1's included org |
| **5: SecurityGroups** | `SecurityGroups.ListAll(runningSpaceGUID)` | `SecurityGroups[].*` | |
| **6: SpaceQuota** | `SpaceQuotas.Get(quotaGUID)` | `SpaceQuota.*` | Only if quota assigned |

#### Simple Models

| Method | V3 API Call | V2 Fields |
|---|---|---|
| `GetOrgs()` | `Organizations.ListAll()` | `Guid`, `Name` |
| `GetSpaces()` | `Spaces.ListAll()` | `Guid`, `Name` |
| `GetOrgUsers(orgName)` | `Organizations.Single()` + `Roles.ListAll(include=user)` | `Guid`, `Username`, `Roles`, `IsAdmin` (always false) |
| `GetSpaceUsers(orgName, spaceName)` | `Organizations.Single()` + `Spaces.Single()` + `Roles.ListAll(include=user)` | `Guid`, `Username`, `Roles`, `IsAdmin` (always false) |

#### Fields Not Available in V3

| V2 Field | Model | Reason |
|---|---|---|
| `IsAdmin` | `GetOrgUsers_Model`, `GetSpaceUsers_Model` | V2 derived from UAA admin scope; V3 roles are CAPI-only |
| `DetectedStartCommand` (distinct from `Command`) | `GetAppModel` | V3 does not distinguish detected vs explicit start command |

### V2 Plugin Model Struct Reference

Complete Go type definitions from [`plugin/models/`](https://github.com/cloudfoundry/cli/tree/main/plugin/models) in the CF CLI source.

#### App Models

```go
// GetAppModel — returned by GetApp(appName string)
type GetAppModel struct {
    Guid                 string
    Name                 string
    BuildpackUrl         string
    Command              string
    DetectedStartCommand string
    DiskQuota            int64 // in Megabytes
    EnvironmentVars      map[string]interface{}
    InstanceCount        int
    Memory               int64 // in Megabytes
    RunningInstances     int
    HealthCheckTimeout   int
    State                string
    SpaceGuid            string
    PackageUpdatedAt     *time.Time
    PackageState         string
    StagingFailedReason  string
    Stack                *GetApp_Stack
    Instances            []GetApp_AppInstanceFields
    Routes               []GetApp_RouteSummary
    Services             []GetApp_ServiceSummary
}

type GetApp_Stack struct {
    Guid, Name, Description string
}

type GetApp_AppInstanceFields struct {
    State     string
    Details   string
    Since     time.Time
    CpuUsage  float64 // percentage
    DiskQuota int64   // in bytes
    DiskUsage int64
    MemQuota  int64
    MemUsage  int64
}

type GetApp_RouteSummary struct {
    Guid   string
    Host   string
    Domain GetApp_DomainFields
    Path   string
    Port   int
}

type GetApp_DomainFields struct {
    Guid, Name string
}

type GetApp_ServiceSummary struct {
    Guid, Name string
}

// GetAppsModel — returned by GetApps()
type GetAppsModel struct {
    Name             string
    Guid             string
    State            string
    TotalInstances   int
    RunningInstances int
    Memory           int64
    DiskQuota        int64
    Routes           []GetAppsRouteSummary
}

type GetAppsRouteSummary struct {
    Guid   string
    Host   string
    Domain GetAppsDomainFields
}

type GetAppsDomainFields struct {
    Guid                   string
    Name                   string
    OwningOrganizationGuid string
    Shared                 bool
}
```

#### Service Models

```go
// GetService_Model — returned by GetService(serviceName string)
type GetService_Model struct {
    Guid            string
    Name            string
    DashboardUrl    string
    IsUserProvided  bool
    ServiceOffering GetService_ServiceFields
    ServicePlan     GetService_ServicePlan
    LastOperation   GetService_LastOperation
}

type GetService_LastOperation struct {
    Type, State, Description string
    CreatedAt, UpdatedAt     string
}

type GetService_ServicePlan struct {
    Name, Guid string
}

type GetService_ServiceFields struct {
    Name, DocumentationUrl string
}

// GetServices_Model — returned by GetServices()
type GetServices_Model struct {
    Guid             string
    Name             string
    ServicePlan      GetServices_ServicePlan
    Service          GetServices_ServiceFields
    LastOperation    GetServices_LastOperation
    ApplicationNames []string
    IsUserProvided   bool
}

type GetServices_LastOperation struct {
    Type, State string
}

type GetServices_ServicePlan struct {
    Guid, Name string
}

type GetServices_ServiceFields struct {
    Name string
}
```

#### Org Models

```go
// GetOrg_Model — returned by GetOrg(orgName string)
type GetOrg_Model struct {
    Guid            string
    Name            string
    QuotaDefinition QuotaFields
    Spaces          []GetOrg_Space
    Domains         []GetOrg_Domains
    SpaceQuotas     []GetOrg_SpaceQuota
}

type GetOrg_Space struct {
    Guid, Name string
}

type GetOrg_Domains struct {
    Guid                   string
    Name                   string
    OwningOrganizationGuid string
    Shared                 bool
}

type GetOrg_SpaceQuota struct {
    Guid                    string
    Name                    string
    MemoryLimit             int64
    InstanceMemoryLimit     int64
    RoutesLimit             int
    ServicesLimit           int
    NonBasicServicesAllowed bool
}

// Shared quota type used by GetOrg_Model and Organization
type QuotaFields struct {
    Guid                    string
    Name                    string
    MemoryLimit             int64
    InstanceMemoryLimit     int64
    RoutesLimit             int
    ServicesLimit           int
    NonBasicServicesAllowed bool
}

// GetOrgs_Model — returned by GetOrgs()
type GetOrgs_Model struct {
    Guid, Name string
}
```

#### Space Models

```go
// GetSpace_Model — returned by GetSpace(spaceName string)
type GetSpace_Model struct {
    GetSpaces_Model                          // embeds Guid, Name
    Organization     GetSpace_Orgs
    Applications     []GetSpace_Apps
    ServiceInstances []GetSpace_ServiceInstance
    Domains          []GetSpace_Domains
    SecurityGroups   []GetSpace_SecurityGroup
    SpaceQuota       GetSpace_SpaceQuota
}

type GetSpace_Orgs struct {
    Guid, Name string
}

type GetSpace_Apps struct {
    Name, Guid string
}

type GetSpace_ServiceInstance struct {
    Guid, Name string
}

type GetSpace_Domains struct {
    Guid                   string
    Name                   string
    OwningOrganizationGuid string
    Shared                 bool
}

type GetSpace_SecurityGroup struct {
    Name  string
    Guid  string
    Rules []map[string]interface{}
}

type GetSpace_SpaceQuota struct {
    Guid                    string
    Name                    string
    MemoryLimit             int64
    InstanceMemoryLimit     int64
    RoutesLimit             int
    ServicesLimit           int
    NonBasicServicesAllowed bool
}

// GetSpaces_Model — returned by GetSpaces()
type GetSpaces_Model struct {
    Guid, Name string
}
```

#### User Models

```go
// GetOrgUsers_Model — returned by GetOrgUsers(orgName string, args ...string)
type GetOrgUsers_Model struct {
    Guid     string
    Username string
    IsAdmin  bool     // Not available in V3 — always false
    Roles    []string
}

// GetSpaceUsers_Model — returned by GetSpaceUsers(orgName, spaceName string)
type GetSpaceUsers_Model struct {
    Guid     string
    Username string
    IsAdmin  bool     // Not available in V3 — always false
    Roles    []string
}
```

#### Context Models (Pass-through — No Wrappers Needed)

```go
// Organization — returned by GetCurrentOrg()
type Organization struct {
    OrganizationFields
}
type OrganizationFields struct {
    Guid            string
    Name            string
    QuotaDefinition QuotaFields
}

// Space — returned by GetCurrentSpace()
type Space struct {
    SpaceFields
}
type SpaceFields struct {
    Guid, Name string
}
```

These context models are populated by the host via RPC and pass through unchanged. They do not need generated wrappers.

## References

- [Plugin Survey](plugin-survey.md) — Survey of 18 actively maintained CF CLI plugins and Rabobank case study
- [cf-plugin-migrate Design](cf-plugin-migrate-design.md) — Detailed design decisions for the migration tool
- [Rabobank cf-plugins](https://github.com/rabobank/cf-plugins) — The production transitional wrapper library
- [CAPI V3 API Documentation](https://v3-apidocs.cloudfoundry.org) — Official Cloud Foundry V3 API reference
- [CAPI V2 API Documentation](https://v2-apidocs.cloudfoundry.org) — Legacy Cloud Foundry V2 API reference (deprecated)
- [CF CLI Plugin Models](https://github.com/cloudfoundry/cli/tree/main/plugin/models) — V2 plugin model type definitions (`plugin_models.*`)
- [go-cfclient](https://github.com/cloudfoundry/go-cfclient) — Cloud Foundry V3 Go client library
- [cf-java-client](https://github.com/cloudfoundry/cf-java-client) — Cloud Foundry Java client library
- [cf-python-client](https://github.com/cloudfoundry-community/cf-python-client) — Cloud Foundry Python client library
- [capi-openapi-spec](https://github.com/cloudfoundry-community/capi-openapi-spec) — Community-maintained OpenAPI spec for CAPI V3
- [cloud_controller_ng#2192](https://github.com/cloudfoundry/cloud_controller_ng/issues/2192) — Tracking issue for official CAPI OpenAPI spec
- [cloudfoundry/cli#3621](https://github.com/cloudfoundry/cli/issues/3621) — Plugin interface tracking issue
