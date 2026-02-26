# Meta
[meta]: #meta
- Name: CLI Plugin Interface V2
- Start Date: 2026-02-26
- Author(s): @norman-abramovitz
- Status: Draft
- RFC Pull Request: [cloudfoundry/community#XXX](https://github.com/cloudfoundry/community/pull/XXX)
- Tracking Issue: [cloudfoundry/cli#3621](https://github.com/cloudfoundry/cli/issues/3621)

## Summary

This RFC proposes a modernized Cloud Foundry CLI plugin interface that replaces the current unmaintained plugin API with a minimal, stable contract. The new interface provides plugins with authentication, session context, and API endpoint information while removing all CF domain models (apps, routes, services) from the plugin API surface. Plugins MUST interact with Cloud Foundry through CAPI V3 directly, using libraries such as [go-cfclient](https://github.com/cloudfoundry/go-cfclient). This approach decouples plugin lifecycle from CLI internals, enables independent plugin evolution, and establishes a sustainable maintenance path as CAPI V2 reaches end of life.

## Problem

The current CF CLI plugin interface suffers from several critical issues that have been identified by CLI maintainers, plugin developers, and plugin users:

### Maintenance and Dependency Issues

1. **Archived and unmaintained dependencies.** The current plugin interface depends on packages that are archived or no longer maintained, creating security and compatibility risks.
2. **Frozen development.** The plugin interface has not been meaningfully updated in years. It has not been published to Go's package registry since November 2019.
3. **No CVE response.** Security vulnerabilities in the plugin framework or its dependencies have not been addressed.
4. **Go language drift.** The interface has not kept pace with Go language evolution, forcing plugin developers to work around outdated patterns.

### Architectural Issues

5. **Tight coupling to CLI internals.** The plugin API exposes methods that proxy CAPI V2 endpoints and return V2-shaped data structures (e.g., `GetApp`, `GetApps`, `GetOrg`, `GetServiceInstances`). These methods embed CF domain semantics into the plugin contract.
6. **V2 API dependency.** With CAPI V2 reaching end of life, plugins that rely on V2-shaped data from the plugin interface will stop working entirely, even though equivalent V3 functionality exists.
7. **Insufficient versioning.** The plugin interface does not provide sufficient semantic versioning support as the CLI version changes.
8. **No language portability.** The current design does not provide a maintainable path to support plugin development in languages other than Go.

### Ecosystem Issues

9. **Plugin repository decay.** The public community CLI plugin repository has become unreliable, with many published plugins unmaintained and lacking information for users to assess safety.
10. **Independent migration burden.** Plugin developers have had to independently discover how to update their plugins for CLI V7, V8, and V9 compatibility without guidance.
11. **Inconsistent UX.** Each plugin implements its own option parsing, leading to inconsistencies between plugins and increased maintenance overhead.

### Evidence from Active Plugin Maintainers

Multiple active plugin maintainers have independently converged on the same minimal integration pattern:

- The **MTA CF CLI plugin** ([cloudfoundry/multiapps-cli-plugin](https://github.com/cloudfoundry/multiapps-cli-plugin)) uses the CLI only for access token, API endpoint, SSL policy, current org/space, and username — all domain operations go through direct CAPI V3 REST calls.
- The **App Autoscaler plugin** ([cloudfoundry/app-autoscaler-cli-plugin](https://github.com/cloudfoundry/app-autoscaler-cli-plugin)) migrated from V2-style plugin API calls to direct V3 client usage, retaining the CLI only for target context and authentication ([PR #132](https://github.com/cloudfoundry/app-autoscaler-cli-plugin/pull/132)).
- The **OCF Scheduler plugin** ([ocf-scheduler-cf-plugin](https://github.com/cloudfoundry-community/ocf-scheduler-cf-plugin)) uses the CLI for login verification, access token, API endpoint, current org/space, user email, and app lookups — then makes all scheduler API calls directly.
- The **cf-java-plugin** maintainer noted that outdated libraries were "a real hassle" and that every plugin has to implement its own option parsing framework.

This convergence demonstrates that the community has already organically adopted the pattern this RFC formalizes.

## Proposal

### Design Principles

1. **CLI as context provider, not domain proxy.** The CLI MUST provide authentication, endpoint, and target context. It MUST NOT provide CF domain models or proxy CAPI endpoints.
2. **Plugin as CAPI consumer.** Plugins MUST own their CAPI V3 interaction, domain logic, and resource mapping.
3. **Minimal stable contract.** The plugin API surface MUST be kept small to minimize breaking changes as the CLI evolves.
4. **Standardized CF access.** The interface SHOULD provide a standardized way to obtain a configured [go-cfclient](https://github.com/cloudfoundry/go-cfclient) V3 client, while not preventing plugins from using alternative libraries.
5. **Backward-compatible transition.** The new interface SHOULD be introduced alongside the existing interface with a documented migration path and deprecation timeline.

### Core Plugin API Contract

The new plugin interface MUST provide the following capabilities:

#### 1. Session and Authentication

| Method | Return Type | Description |
|---|---|---|
| `AccessToken()` | `(string, error)` | Current OAuth access token |
| `RefreshToken()` | `(string, error)` | Current OAuth refresh token |
| `IsLoggedIn()` | `(bool, error)` | Whether a user session is active |
| `Username()` | `(string, error)` | Authenticated user's username |

#### 2. API Endpoint and Configuration

| Method | Return Type | Description |
|---|---|---|
| `ApiEndpoint()` | `(string, error)` | CF API URL (full URL including scheme) |
| `HasAPIEndpoint()` | `(bool, error)` | Whether an API endpoint is configured |
| `IsSSLDisabled()` | `(bool, error)` | Whether SSL certificate verification is disabled |
| `ApiVersion()` | `(string, error)` | CF API version string |

#### 3. Target Context

| Method | Return Type | Description |
|---|---|---|
| `GetCurrentOrg()` | `(OrgContext, error)` | Current targeted org (GUID and name) |
| `GetCurrentSpace()` | `(SpaceContext, error)` | Current targeted space (GUID and name) |
| `HasOrganization()` | `(bool, error)` | Whether an org is currently targeted |
| `HasSpace()` | `(bool, error)` | Whether a space is currently targeted |

The context types MUST be minimal:

```go
type OrgContext struct {
    GUID string
    Name string
}

type SpaceContext struct {
    GUID string
    Name string
}
```

#### 4. Plugin Registration

| Method | Description |
|---|---|
| `GetMetadata() PluginMetadata` | Return plugin name, version, and command definitions |
| `Run(connection PluginContext, args []string)` | Entry point invoked by the CLI |

The `PluginMetadata` struct MUST support:

```go
type PluginMetadata struct {
    Name          string
    Version       PluginVersion
    MinCliVersion PluginVersion   // Minimum compatible CLI version
    Commands      []Command
}

type PluginVersion struct {
    Major int
    Minor int
    Build int

    // Pre-release and build metadata per semver 2.0
    PreRelease string
    BuildMeta  string
}

type Command struct {
    Name      string
    Alias     string
    HelpText  string
    UsageDetails Usage
}

type Usage struct {
    Usage   string
    Options map[string]string
}
```

### Methods Explicitly Removed from the Plugin API

The following categories of methods from the current plugin interface MUST NOT be carried forward, as they embed CF domain models into the plugin contract:

- **Application methods:** `GetApp`, `GetApps` — plugins MUST use CAPI V3 directly
- **Service methods:** `GetServices`, `GetService`, `GetServiceInstances` — plugins MUST use CAPI V3 directly
- **Organization methods:** `GetOrg`, `GetOrgs` — plugins MUST use CAPI V3 to query beyond current context
- **Space methods:** `GetSpace`, `GetSpaces` — plugins MUST use CAPI V3 to query beyond current context
- **Route methods:** `GetRoutes` — plugins MUST use CAPI V3 directly
- **CLI command execution:** `CliCommand`, `CliCommandWithoutTerminalOutput` — plugins MUST NOT depend on CLI command output parsing

### Standardized CF Client Access

The plugin interface SHOULD provide a convenience method to obtain a pre-configured [go-cfclient](https://github.com/cloudfoundry/go-cfclient) V3 client:

```go
// CfClient returns a configured go-cfclient v3 client using the current
// session's access token, API endpoint, and SSL configuration.
//
// This is the RECOMMENDED way for plugins to interact with Cloud Foundry.
// Plugins MAY use alternative libraries, but go-cfclient provides
// canonical CF API V3 models and is maintained by the Cloud Foundry community.
CfClient() (*cfclient.Client, error)
```

This standardization:
- Eliminates duplicate HTTP client setup code across plugins
- Provides a single, community-maintained source for CAPI V3 models
- Reduces the barrier to entry for new plugin developers
- Ensures plugins automatically benefit from go-cfclient updates

Plugins are NOT required to use this client — they MAY use any HTTP client or CF library — but this SHOULD be the documented and recommended path.

### Additional Endpoint Access

The plugin interface SHOULD provide methods to discover related CF platform service endpoints:

| Method | Return Type | Description |
|---|---|---|
| `UaaEndpoint()` | `(string, error)` | UAA server URL for direct token operations |
| `LoggregatorEndpoint()` | `(string, error)` | Log aggregator endpoint |
| `DopplerEndpoint()` | `(string, error)` | Doppler WebSocket endpoint |
| `RoutingApiEndpoint()` | `(string, error)` | Routing API endpoint |

These endpoints enable plugins to integrate with platform services beyond the Cloud Controller without having to discover endpoints independently.

### Enhanced Plugin Metadata

#### Semantic Versioning

Plugin versions MUST follow [Semantic Versioning 2.0.0](https://semver.org/). The `PluginVersion` struct includes `PreRelease` and `BuildMeta` fields to support full semver compliance. The CLI MUST display this information in plugin listings and SHOULD warn users when a plugin's `MinCliVersion` exceeds the current CLI version.

#### Improved Help Integration

The CLI SHOULD support:
- Viewing help for a single installed plugin: `cf help <plugin-name>`
- Grouping plugin commands separately in `cf help` output
- Long-form descriptions in command help beyond the current single-line `HelpText`

### Plugin Repository Improvements

While a full plugin repository redesign is outside the scope of this RFC, the following requirements inform future work:

1. Plugins SHOULD declare their minimum compatible CLI version.
2. Plugins SHOULD declare their minimum compatible CAPI version.
3. The plugin repository SHOULD implement a deprecation process for plugins that have not been updated within a defined period.
4. The plugin repository SHOULD provide build and dependency metadata to help users assess plugin safety.

### Migration Path

#### Phase 1: Introduce New Interface (Target: Q3 2026)

- Publish the new plugin interface as a standalone Go module (e.g., `code.cloudfoundry.org/cli-plugin-api/v2`).
- The new module MUST have zero dependencies on the CLI's internal packages.
- Document migration guides with before/after examples.
- Provide a reference plugin implementation demonstrating the recommended pattern.

#### Phase 2: Dual Support (Target: Q4 2026)

- The CLI MUST support both the legacy and new plugin interfaces simultaneously.
- New plugins SHOULD use the new interface.
- Existing plugins continue to work without modification.

#### Phase 3: Deprecation (Target: Q1 2027)

- The legacy plugin interface is formally deprecated.
- The CLI emits warnings when loading plugins that use the legacy interface.
- Plugin repository begins flagging plugins that use the deprecated interface.

#### Phase 4: Removal (Target: Q3 2027 or later)

- The legacy plugin interface is removed from the CLI.
- All actively maintained plugins are expected to have migrated.

### Reference Architecture

The following diagram illustrates the recommended plugin architecture:

```
┌─────────────────────────────────────────────────────┐
│                    CF CLI                            │
│                                                     │
│  ┌───────────────────────────────────────────────┐  │
│  │           Plugin Host Interface                │  │
│  │                                                │  │
│  │  Session:  AccessToken, RefreshToken,          │  │
│  │           IsLoggedIn, Username                 │  │
│  │                                                │  │
│  │  Endpoint: ApiEndpoint, IsSSLDisabled,         │  │
│  │           ApiVersion, UaaEndpoint              │  │
│  │                                                │  │
│  │  Context:  GetCurrentOrg, GetCurrentSpace,     │  │
│  │           HasOrganization, HasSpace            │  │
│  │                                                │  │
│  │  Client:   CfClient() → go-cfclient v3        │  │
│  │                                                │  │
│  │  Register: GetMetadata, Run                    │  │
│  └───────────────────────────────────────────────┘  │
└────────────────────┬────────────────────────────────┘
                     │
                     │ Plugin API Contract
                     │ (minimal, stable)
                     │
┌────────────────────▼────────────────────────────────┐
│                  Plugin                              │
│                                                     │
│  ┌─────────────┐  ┌─────────────────────────────┐  │
│  │  Plugin      │  │  Domain Logic                │  │
│  │  Commands    │  │                              │  │
│  │             │──▶│  Uses go-cfclient V3 or     │  │
│  │  Registration│  │  custom HTTP client to       │  │
│  │  Help text   │  │  interact with CAPI V3      │  │
│  │  Flag parsing│  │  and other platform APIs     │  │
│  └─────────────┘  └──────────────┬──────────────┘  │
└───────────────────────────────────┼──────────────────┘
                                    │
                          Direct CAPI V3 calls
                                    │
                                    ▼
                     ┌──────────────────────────┐
                     │  Cloud Controller V3 API  │
                     │  UAA, Doppler, etc.       │
                     └──────────────────────────┘
```

### Example: Migrating a Plugin

**Before (legacy interface):**

```go
func (p *MyPlugin) Run(cli plugin.CliConnection, args []string) {
    // Get app using CLI's V2-coupled method
    app, _ := cli.GetApp("my-app")
    fmt.Println(app.Guid)

    // Get services using CLI's V2-coupled method
    services, _ := cli.GetServices()
    for _, s := range services {
        fmt.Println(s.Name)
    }
}
```

**After (new interface):**

```go
func (p *MyPlugin) Run(ctx pluginapi.PluginContext, args []string) {
    // Get a configured V3 client
    client, _ := ctx.CfClient()

    // Get current space from context
    space, _ := ctx.GetCurrentSpace()

    // Query apps directly via CAPI V3
    apps, _ := client.Applications.ListAll(context.Background(),
        &cfclient.AppListOptions{SpaceGUIDs: cfclient.Filter{Values: []string{space.GUID}}},
    )
    for _, app := range apps {
        fmt.Println(app.GUID, app.Name)
    }
}
```

### Future Considerations

The following topics are acknowledged but deferred to separate RFCs:

- **Polyglot plugin support** (HashiCorp-style gRPC plugin model) — enables plugins in languages other than Go.
- **GitHub-style plugin distribution** — trust model, signing, and automated security scanning.
- **CLI adoption of go-cfclient internally** — centralizing CAPI interaction across CLI and plugins.
- **Standard option parsing** — providing a shared flag parsing framework to improve UX consistency across plugins.

## References

- [cloudfoundry/cli#3621 — New Plugin Interface](https://github.com/cloudfoundry/cli/issues/3621)
- [app-autoscaler-cli-plugin PR #132 — Switch to V3 CF API client](https://github.com/cloudfoundry/app-autoscaler-cli-plugin/pull/132)
- [go-cfclient — Cloud Foundry V3 Go client library](https://github.com/cloudfoundry/go-cfclient)
- [Current plugin interface — code.cloudfoundry.org/cli/plugin](https://pkg.go.dev/code.cloudfoundry.org/cli/plugin)
- [Semantic Versioning 2.0.0](https://semver.org/)
