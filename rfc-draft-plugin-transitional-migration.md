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

Note: Unlike the Rabobank library, this companion package does **not** reimplement V2 domain methods via V3. Attempting to fit V3 responses into V2-shaped types is lossy and inefficient (see [Rabobank caveats](plugin-survey.md#caveats-and-limitations)). Plugins SHOULD use go-cfclient directly for domain operations.

## Lessons from the Rabobank Implementation

The Rabobank `cf-plugins` library validates this approach but also reveals pitfalls to avoid:

### What Worked

- **Zero host changes.** The library wraps `plugin.CliConnection` and calls `plugin.Start()` — compatible with any CF CLI version.
- **Incremental adoption.** Consumer plugins adopt at their own pace — 2 of 4 Rabobank consumers use the library.
- **Two migration tiers.** `plugins.Start()` provides transparent V3 reimplementation; `plugins.Execute()` provides direct `CfClient()` access.

### What to Avoid

1. **Do not reimplement V2 domain methods via V3.** Rabobank's `GetApp()` requires 11 API calls and cannot represent multi-process apps, multiple buildpacks, or detailed instance stats. The V2 model types are structurally inadequate for V3 data. Plugins SHOULD use go-cfclient directly and work with V3 resource types.

2. **Do not strip the token prefix manually.** Rabobank uses `token[7:]` to strip `"bearer "`. The go-cfclient `config.Token()` function handles this internally. Manual stripping is fragile if the prefix format ever changes.

3. **Do not hardcode SSL settings.** Rabobank calls `config.SkipTLSValidation()` unconditionally. The companion package SHOULD pass through the host's `IsSSLDisabled()` value.

4. **Do not hardcode user agent strings.** Rabobank uses `config.UserAgent("cfs-plugin/1.0.9")`. The companion package SHOULD derive the user agent from the plugin's metadata (name and version).

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
