# Meta
[meta]: #meta
- Name: CF CLI Plugin Interface Modernization
- Start Date: 2026-03-01
- Author(s): @norman-abramovitz
- Status: Draft
- RFC Pull Request: TBD (to replace or supersede [community#1452](https://github.com/cloudfoundry/community/pull/1452))
- Tracking Issue: [cloudfoundry/cli#3621](https://github.com/cloudfoundry/cli/issues/3621)

## Summary

The CF CLI plugin interface depends on CAPI V2, which is reaching end of life per [RFC-0032](https://github.com/cloudfoundry/community/blob/main/toc/rfc/rfc-0032-capi-v2-removal.md). When V2 endpoints are removed, at least 20 actively maintained plugins will break. This RFC defines the complete modernization path for the CF CLI plugin ecosystem: deprecation of V2 domain methods and `CliCommand` passthrough in CLI v8, a guest-side transitional migration technique that plugin teams can adopt immediately without CLI changes, a modernized plugin interface for CLI v9 with a minimal stable contract and polyglot support, and a plugin repository metadata and maintenance policy. Together, these four parts unblock CAPI V2 removal, eliminate legacy coupling, and establish a sustainable plugin architecture.

## Problem

### 1. V2 Domain Methods Will Break

The current plugin interface exposes 10 methods that return CAPI V2-shaped data via RPC from the host CLI process:

| Method | V2 Dependency | Plugins Using |
|--------|---------------|---------------|
| `GetApp(name)` | `/v2/apps` via command runner | OCF Scheduler, metric-registrar, list-services |
| `GetApps()` | `/v2/apps` via command runner | top, OCF Scheduler, metric-registrar, spring-cloud-services |
| `GetService(name)` | `/v2/service_instances` via command runner | service-instance-logs, spring-cloud-services, mysql-cli, Swisscom, cf-service-connect |
| `GetServices()` | `/v2/service_instances` via command runner | metric-registrar, html5-apps-repo |
| `GetOrg(name)` | `/v2/organizations` via command runner | Swisscom, html5-apps-repo |
| `GetOrgs()` | `/v2/organizations` via command runner | stack-auditor |
| `GetSpaces()` | `/v2/spaces` via command runner | (inferred from org context) |
| `GetOrgUsers(orgName, roles...)` | `/v2/organizations/:guid/users` via command runner | role-management plugins |
| `GetSpaceUsers(spaceName, roles...)` | `/v2/spaces/:guid/users` via command runner | role-management plugins |
| `GetSpace(name)` | `/v2/spaces` via command runner | Swisscom, html5-apps-repo |

Each method works by running a full internal CLI command via `commandregistry`, injecting a `PluginModels` output pointer, and returning V2-shaped `plugin_models.*` structs. When CAPI V2 is removed per RFC-0032, these methods will fail.

Additionally, `CliCommand` and `CliCommandWithoutTerminalOutput` allow plugins to run arbitrary CLI commands including `cf curl` against V2 endpoints. A scan of 20 plugins found `CliCommand` usage across 14 plugins, with patterns including `cf curl /v2/apps`, `cf push`, `cf bind-service`, and pagination over V2 list endpoints.

### 2. Host Carries Legacy Code

The CF CLI host process maintains a substantial codebase solely to serve plugin domain methods:

| File | Coupling |
|------|----------|
| `plugin/rpc/cli_rpc_server.go` | `cf/api.RepositoryLocator`, `cf/commandregistry`, `cf/configuration/coreconfig`, `cf/terminal` |
| `plugin/rpc/run_plugin.go` | `cf/configuration/pluginconfig` |
| `plugin/rpc/call_command_registry.go` | `cf/commandregistry`, `cf/flags`, `cf/requirements` |
| `plugin/models/*.go` (13 files) | V2-shaped types mirroring CAPI V2 responses |

This code cannot be removed until plugins stop depending on V2 domain methods. It creates maintenance burden and a class of RPC-related bugs.

### 3. Plugins Already Converged on a Minimal Pattern

A survey of 20 actively maintained plugins reveals that the community has organically converged on using the CLI as an identity and context provider, not a domain proxy:

| Method | Usage | Observation |
|--------|-------|-------------|
| `GetCurrentSpace()` | 14/20 | Most widely used context method |
| `AccessToken()` | 13/20 | Universal for plugins making direct API calls |
| `ApiEndpoint()` | 12/20 | Universal for URL construction and client initialization |
| `Username()` | 10/20 | Primarily for display purposes |
| `IsSSLDisabled()` | 9/20 | Required for TLS configuration |
| `GetCurrentOrg()` | 8/20 | Common but not universal |
| `IsLoggedIn()` | 7/20 | Guard check before proceeding |

Domain methods are being actively abandoned. The App Autoscaler plugin removed `GetApp()` in [PR #132](https://github.com/cloudfoundry/app-autoscaler-cli-plugin/pull/132) and replaced it with go-cfclient V3 direct calls. The MTA plugin uses direct CAPI V3 REST calls for all domain operations. The cf-java-plugin abandoned `CliCommand` entirely due to reliability issues (`cf ssh` via the plugin API fails where the direct CLI succeeds) and now uses `exec.Command("cf", ...)` for all CF interaction.

### 4. Plugins Import CLI Internal Packages

Beyond the intended public interface (`plugin/` and `plugin/models/`), 11 of 20 surveyed plugins import internal CLI packages, creating build-time coupling to code the CLI team never intended to expose:

| Internal Package | Plugins | Purpose |
|------------------|---------|---------|
| `cf/terminal` | 6 | Colored/formatted terminal output |
| `cf/trace` | 6 | HTTP request tracing/debug logging |
| `cf/configuration/confighelpers` | 4 | Config file path discovery (`DefaultFilePath()`, `PluginRepoDir()`) |
| `cf/i18n` | 2 | Internationalization (both set `i18n.T` to a no-op) |
| `cf/formatters` | 1 | `ByteSize()` formatting |
| `cf/flags` | 1 | `FlagContext` type |
| `cf/configuration` + `coreconfig` | 1 | Direct `~/.cf/config.json` read/write (cf-targets) |
| `util/configv3` | 1 | V3 config layer (mysql-cli) |
| `util/ui` | 1 | UI rendering (mysql-cli) |

These packages are currently frozen -- analysis of the CF CLI git history shows zero exported API changes in `cf/configuration/confighelpers` since 2020, and only test infrastructure updates in `cf/terminal`, `cf/trace`, `cf/formatters`, `cf/i18n`, and `cf/flags`. The coupling has not broken plugins *yet* because the CLI team has not refactored these packages. Any future refactoring would break up to 11 plugins with no warning.

The module path migration from `code.cloudfoundry.org/cli` to `code.cloudfoundry.org/cli/v8` is an additional breaking change for plugins still pinned to `v7.1.0+incompatible`.

## Proposal

This proposal is organized into four parts. Each part addresses a distinct aspect of the modernization, but they are designed to work together as a coordinated program.

### Part 1: Deprecation of V2 Domain Methods (CLI v8)

#### Scope

Part 1 applies to CF CLI v8 and later. CF CLI v7 is already deprecated and MUST NOT receive new deprecation warnings.

#### Methods to Deprecate

The following methods on the `plugin.CliConnection` interface MUST be formally deprecated:

**V2 domain methods (10):**
- `GetApp(appName string)` -- returns `plugin_models.GetAppModel`
- `GetApps()` -- returns `[]plugin_models.GetAppsModel`
- `GetService(serviceInstance string)` -- returns `plugin_models.GetService_Model`
- `GetServices()` -- returns `[]plugin_models.GetServices_Model`
- `GetOrg(orgName string)` -- returns `plugin_models.GetOrg_Model`
- `GetOrgs()` -- returns `[]plugin_models.GetOrgs_Model`
- `GetSpaces()` -- returns `[]plugin_models.GetSpaces_Model`
- `GetOrgUsers(orgName string, roles ...string)` -- returns `[]plugin_models.GetOrgUsers_Model`
- `GetSpaceUsers(spaceName string, roles ...string)` -- returns `[]plugin_models.GetSpaceUsers_Model`
- `GetSpace(spaceName string)` -- returns `plugin_models.GetSpace_Model` (discovered by scanner -- missed in initial manual survey)

**CLI passthrough methods (2):**
- `CliCommand(args ...string)` -- runs arbitrary CLI commands, captures output
- `CliCommandWithoutTerminalOutput(args ...string)` -- same, suppresses terminal

These 12 methods embed CF domain models into the plugin contract or create fragile coupling to CLI command output format. They MUST NOT be carried forward to the modernized interface.

#### Deprecation Timeline

| Milestone | Target | Action |
|-----------|--------|--------|
| Announce | Q2 2026 | CLI v8 emits runtime deprecation warnings when V2 domain methods or `CliCommand` are called. Warnings include migration guidance pointing to Part 2 of this RFC. |
| Migration period | Q2--Q4 2026 | Plugin developers migrate using the guest-side technique (Part 2) or direct V3 access. |
| Removal | Aligned with RFC-0032 | V2 domain method implementations and `CliCommand` support removed from the host. Exact date follows RFC-0032 CAPI V2 removal timeline. |

The CLI team MUST NOT remove these methods before CAPI V2 is removed. The deprecation warnings serve as advance notice, giving plugin developers time to migrate before the methods stop functioning.

#### CLI Version Support Lines

| CLI Version | Plugin Interface Status |
|-------------|------------------------|
| v7 | Deprecated CLI. Plugin interface unchanged. No new warnings. |
| v8 | Current. Deprecation warnings added for V2 domain methods. All methods remain functional. |
| v9 (future) | Modernized interface (Part 3). Legacy methods available only via backward-compatible channel. |

### Part 2: Guest-Side Transitional Migration (Immediate, No CLI Changes Required)

#### Rationale: Why Guest-Side?

Three independent teams converged on the same guest-side pattern before this RFC existed:

1. **Rabobank** ([rabobank/cf-plugins](https://github.com/rabobank/cf-plugins)) -- production since 2025. Created a V2 compatibility library reimplementing all domain methods via go-cfclient V3.
2. **App Autoscaler** ([PR #132](https://github.com/cloudfoundry/app-autoscaler-cli-plugin/pull/132)) -- removed `GetApp()`, replaced with go-cfclient V3 direct calls. Defined a custom `Connection` interface with only 6 methods.
3. **MTA/MultiApps** -- all domain operations go through direct CAPI V3 REST calls. The plugin interface is used only for authentication and context.

A host-side alternative (the CLI team reimplements domain methods against V3 and continues serving them via RPC) would perpetuate the coupling between plugin lifecycle and CLI internals. The guest-side approach eliminates the coupling entirely -- each plugin team migrates at their own pace, and the CLI team removes legacy code on its own timeline.

#### Architecture

The transitional approach introduces a thin wrapper on the guest side:

```
Host (CF CLI)                              Guest (Plugin)
   │                                         │
   │  Existing gob/net-rpc protocol          │
   │  (no changes required)                  │
   │                                         │
   │  Context methods ◄──── pass-through ────┤
   │  (AccessToken,                          │
   │   ApiEndpoint, etc.)                    │
   │                                         │
   │                         ┌───────────────┤
   │                         │ V2Compat      │
   │                         │ (generated)   │
   │                         │               │
   │                         │ go-cfclient   │
   │                         │ V3 calls for  │
   │                         │ domain methods│
   │                         └───────┬───────┘
   │                                 │
   │                                 ▼
   │                         Cloud Controller V3
```

The wrapper:
- **Embeds** `plugin.CliConnection` -- all 16 context methods pass through unchanged via RPC to the host
- **Constructs** a go-cfclient V3 `*client.Client` from `AccessToken()`, `ApiEndpoint()`, and `IsSSLDisabled()`
- **Reimplements** only the V2 domain methods the plugin actually uses, backed by the minimum V3 API calls required for the fields the plugin accesses
- **Satisfies** `plugin.CliConnection` -- existing code that accepts the connection interface works without changes

#### The `cf-plugin-migrate` Tool

A two-command CLI tool automates the migration:

**`cf-plugin-migrate scan`** -- AST-based audit of Go source code. Detects:
1. V2 domain method calls and which fields of the returned model are actually accessed
2. `CliCommand` / `CliCommandWithoutTerminalOutput` calls with command extraction, `cf curl` endpoint analysis, `json.Unmarshal` tracing, and V2-to-V3 endpoint mapping for 20 known V2 paths
3. Session/context method calls (no migration needed)
4. Internal CLI package imports with replacement suggestions

Output: human-readable summary (stderr) + YAML configuration (stdout).

**`cf-plugin-migrate generate`** -- reads the YAML configuration and produces `v2compat_generated.go`:
- `V2Compat` struct embedding `plugin.CliConnection`
- `NewV2Compat(conn)` constructor building a go-cfclient V3 client from connection credentials
- Reimplementations of only the declared V2 domain methods, using only the V3 API calls required for the declared fields
- CF_TRACE-aware HTTP tracing injected automatically via `cftrace.NewTracingTransport`

The generator uses V3 `include` and `fields` query parameters where CAPI V3 supports them, collapsing multiple dependency groups into single calls:

| Method | Without optimization | With `include`/`fields` | Savings |
|--------|---------------------|------------------------|---------|
| `GetService` | 3 calls (instance + plan + offering) | 1 call with `fields[service_plan]` + `fields[service_plan.service_offering]` | 2 calls eliminated |
| `GetServices` | 1 + 3xN calls (per-instance) | 2 calls (list with `fields` + bindings with `include=app`) | 3xN calls reduced to 2 |
| `GetSpace` | 7 calls (separate org GET) | 6 calls with `include=organization` on spaces | 1 call eliminated |

#### Two Migration Paths

**Path A: Generated V2Compat Wrapper (Recommended)**

```bash
# Step 1: Scan
cd your-plugin/
cf-plugin-migrate scan ./... > cf-plugin-migrate.yml

# Step 2: Review and adjust YAML

# Step 3: Generate
cf-plugin-migrate generate

# Step 4: Add dependency (domain method plugins only)
go get github.com/cloudfoundry/go-cfclient/v3

# Step 5: Wire the wrapper (one line in Run)
```

```go
func (p *MyPlugin) Run(conn plugin.CliConnection, args []string) {
    conn, err := NewV2Compat(conn) // shadow the parameter
    if err != nil { fmt.Println(err); return }
    // All existing code works unchanged -- conn.GetApp() now uses V3
}
```

Session-only plugins require zero code changes. The generated file compiles alongside existing code with no modifications. Validated with cf-targets-plugin: zero lines of plugin code changed.

Domain method plugins require one new dependency (`go-cfclient/v3`) and minimal code changes. Validated with OCF Scheduler against live CAPI V3 (v3.180.0): `cf create-job` resolved app via V3 `Applications.Single`.

**Path B: Direct V3 Access (For New Development)**

Plugins starting fresh or developers who want V3-native types bypass the wrapper entirely:

```go
func (p *MyPlugin) Run(conn plugin.CliConnection, args []string) {
    endpoint, _ := conn.ApiEndpoint()
    token, _ := conn.AccessToken()
    skipSSL, _ := conn.IsSSLDisabled()

    cfg, _ := config.New(endpoint,
        config.Token(token),
        config.SkipTLSValidation(skipSSL),
    )
    cfClient, _ := client.New(cfg)

    // Use V3-native types directly
    app, _ := cfClient.Applications.Single(ctx, &client.AppListOptions{...})
}
```

#### Companion Packages for Internal Import Decoupling

Plugins that import internal CLI packages SHOULD migrate to standalone replacement packages in `code.cloudfoundry.org/cf-plugin-helpers`:

| Package | Replaces | Functions | Plugins |
|---------|----------|-----------|---------|
| `cfconfig` | `cf/configuration/confighelpers` | `DefaultFilePath()`, `PluginRepoDir()` | 4 |
| `cftrace` | `cf/trace` | `NewLogger()`, `NewWriterPrinter()`, `NewTracingTransport()` | 6 |
| `cfui` | `cf/terminal` (Pattern B) | `Say()`, `Warn()`, `Failed()`, `Ok()`, `Table()`, color functions | 5 |
| `cfformat` | `cf/formatters` | `ByteSize()` | 1 |

Migration is an import-path swap with aliases to preserve existing references:

```go
// Before
import "code.cloudfoundry.org/cli/cf/trace"

// After
import trace "code.cloudfoundry.org/cf-plugin-helpers/cftrace"
```

Validated with app-autoscaler-cli-plugin: 4 import lines changed across 4 files, zero code changes, all tests pass.

**Not covered by companion packages** (per-plugin remediation):
- `cf/terminal` Pattern A (multiapps only -- 16 files, full UI framework with 30+ `EntityNameColor` calls)
- `cf/configuration` + `coreconfig` (cf-targets only -- direct `~/.cf/config.json` read/write with undocumented format)

#### Proof-of-Concept Validation Tiers

| Tier | Plugin | Complexity | Status |
|------|--------|------------|--------|
| 0 | cf-targets-plugin | Session-only, zero code changes | Validated |
| 1 | list-services | 1 domain method (`GetApp` for GUID only) | Analyzed, migration documented |
| 2 | OCF Scheduler | `GetApp` with domain logic, `CliCommand` for workflow | Validated against live CAPI V3 |
| 3 | metric-registrar | Multiple domain methods, `cf curl`, complex field usage | Analyzed, migration documented |

### Part 3: Modernized Plugin Interface (CLI v9, Future)

#### Design Principles

1. **Host as context provider, not domain proxy.** The host MUST provide authentication, endpoint, and target context. It MUST NOT provide CF domain models or proxy CAPI endpoints.
2. **Guest as CAPI consumer.** Guests MUST own their CAPI V3 interaction, domain logic, and resource mapping.
3. **Minimal stable contract.** The plugin API surface MUST be kept small to minimize breaking changes.
4. **Protocol-agnostic communication.** Host-to-guest communication MUST be abstracted behind a channel interface.
5. **Language portability.** The plugin interface MUST NOT require guests to be written in Go.
6. **Backward-compatible transition.** The new interface SHOULD be introduced alongside the existing interface with a documented migration path.

#### Core Plugin API Contract: PluginContext

The modernized interface replaces `CliConnection` with `PluginContext`, containing only serializable primitives that can cross a process boundary over any wire protocol:

**Session and Authentication:**

| Method | Return Type | Description |
|--------|-------------|-------------|
| `AccessToken()` | `(string, error)` | Current OAuth access token |
| `RefreshToken()` | `(string, error)` | Current OAuth refresh token |
| `IsLoggedIn()` | `(bool, error)` | Whether a user session is active |
| `Username()` | `(string, error)` | Authenticated user's username |

**API Endpoint and Configuration:**

| Method | Return Type | Description |
|--------|-------------|-------------|
| `ApiEndpoint()` | `(string, error)` | CF API URL (full URL including scheme) |
| `HasAPIEndpoint()` | `(bool, error)` | Whether an API endpoint is configured |
| `IsSSLDisabled()` | `(bool, error)` | Whether SSL certificate verification is disabled |
| `ApiVersion()` | `(string, error)` | CF API version string |

**Target Context:**

| Method | Return Type | Description |
|--------|-------------|-------------|
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

#### CF Client Access (Companion Package)

`CfClient()` MUST NOT be part of the core plugin contract. A companion Go package (`code.cloudfoundry.org/cli-plugin-helpers/cfclient`) SHOULD provide `NewCfClient(ctx PluginContext)` that constructs a go-cfclient V3 client from the core contract primitives. This keeps the core contract dependency-free and language-agnostic.

```
Layer 1: Host <-> Guest hosting (channel abstraction: Send/Receive/Open/Close)
Layer 2: Core contract (serializable primitives: tokens, endpoints, context)
Layer 3: Guest <-> Backend service (guest's choice: go-cfclient, gRPC, HTTP, etc.)
```

#### Channel Abstraction

The host MUST abstract all guest communication behind a channel interface:

```go
type PluginChannel interface {
    Open() error
    Send(msg Message) error
    Receive() (Message, error)
    Close() error
}
```

Two concrete implementations:

| Implementation | Transport | Serialization | Use Case |
|----------------|-----------|---------------|----------|
| `GobTCPChannel` | TCP localhost | `encoding/gob` | Legacy Go guests (backward compat) |
| `JsonRpcChannel` | TCP localhost | JSON-RPC 2.0 | New polyglot guests |

The legacy `net/rpc` transport is preserved -- it is serviceable plumbing. It can carry new JSON-RPC payloads alongside existing gob-encoded methods. `stdout` and `stderr` MUST remain available for the guest's user-facing output. The JSON-RPC protocol channel MUST use a separate transport (TCP connection).

#### JSON-RPC 2.0 for Polyglot Support

New guests MUST use [JSON-RPC 2.0](https://www.jsonrpc.org/specification) as the message format, enabling plugin development in any language that can read/write JSON. JSON-RPC provides bidirectional request/response, notifications, standardized error codes, and universal language support.

```
Host                                        Guest
 |  {"jsonrpc":"2.0","method":"Run",          |
 |   "params":{"command":"create-job",...},    |
 |   "id":1}                                  |
 |---------------------------------------------->
 |                                            |
 |  {"jsonrpc":"2.0","method":"AccessToken",  |
 |   "id":100}                                |
 |<----------------------------------------------
 |                                            |
 |  {"jsonrpc":"2.0","result":"bearer eyJ..", |
 |   "id":100}                                |
 |---------------------------------------------->
```

#### Embedded Metadata Marker (No Execution at Install Time)

Guests MUST embed a `CF_PLUGIN_METADATA:` marker string followed by a JSON object in the guest file (binary or script). The host scans the file for this marker and extracts metadata directly, without executing the guest:

**Compiled Go binary:**
```go
var _ = `CF_PLUGIN_METADATA:{"schema_version":1,"name":"AutoScaler","protocol":"jsonrpc","version":{"major":4,"minor":1,"patch":2},"commands":[...]}`
```

**Python script:**
```python
#!/usr/bin/env python3
# CF_PLUGIN_METADATA:{"schema_version":1,"name":"my-plugin","protocol":"jsonrpc",...}
```

Install flow:
1. Read guest file (binary or script)
2. Scan for `CF_PLUGIN_METADATA:` marker
3. Found: parse JSON, store metadata + protocol in config, copy to plugins dir
4. Not found: legacy Go guest, use existing exec + gob/net-rpc metadata exchange

The marker survives in compressed and self-extracting executables (UPX overlay data, PE resources, shell script headers, 7-Zip SFX config blocks, Mach-O custom segments). All JSON characters are safe to embed in any binary format.

#### Improved Versioning

The current `VersionType` struct uses only `Major`, `Minor`, and `Build` integer fields. The `Build` field name is a misnomer (it corresponds to SemVer's "patch" number). Plugins that need prerelease identifiers or build metadata cannot express them through the plugin API.

```go
type PluginVersion struct {
    Major      int
    Minor      int
    Patch      int         // Renamed from Build for SemVer correctness
    PreRelease string      // e.g., "rc.1", "beta.2"
    BuildMeta  string      // e.g., "linux.amd64", "20260301"
}

func (v PluginVersion) String() string // Full SemVer 2.0 string
```

#### Improved Help Metadata

The modernized `Command` struct adds optional fields aligning with the [CF CLI Help Guidelines](https://github.com/cloudfoundry/cli/wiki/CF-CLI-Help-Guidelines):

```go
type Command struct {
    Name         string
    Alias        string
    HelpText     string        // Short one-line description
    Description  string        // Long-form description (optional)
    Warning      string        // Critical alerts (optional)
    Examples     string        // Usage examples (optional)
    Tip          string        // Helpful context or deprecation notices (optional)
    RelatedCmds  []string      // "See also" commands (optional)
    UsageDetails Usage
}

type Usage struct {
    Usage   string
    Options map[string]string       // Legacy (unordered)
    Flags   []FlagDefinition        // Preferred (structured, ordered)
}

type FlagDefinition struct {
    Long        string    // e.g., "output"
    Short       string    // e.g., "o"
    Description string
    Default     string    // e.g., "json"
    HasArg      bool
    Required    bool
    Group       string    // e.g., "Output options"
}
```

When `Flags` is populated, the host MUST use it for help display instead of `Options`. When only `Options` is populated, the current behavior is preserved.

The host SHOULD also implement:
- `cf help <plugin-name>` -- display all commands from a specific plugin
- Grouped display in `cf help -a` -- commands organized by plugin, not flat alphabetical

#### CliCommand / CliCommandWithoutTerminalOutput Not Carried Forward

These methods MUST NOT be part of the new JSON-RPC contract. They ask the host to execute arbitrary CLI commands on the plugin's behalf, creating tight coupling to CLI command names, output format, and behavior across versions. With the new contract providing session credentials and endpoint URLs, plugins MUST call CAPI directly using their own HTTP clients or client libraries.

Legacy guests retain `CliCommand` access via the `GobTCPChannel` backward-compatibility path.

#### Migration Phases

| Phase | Target | Description |
|-------|--------|-------------|
| Phase 0 | Available now | Guest-side transitional migration (Part 2). No CLI changes required. |
| Phase 1 | Q3 2026 | Channel abstraction (`PluginChannel` interface), `GobTCPChannel` wrapping existing transport, `CF_PLUGIN_METADATA:` marker scanning in `cf install-plugin`, core contract published as standalone Go module. |
| Phase 2 | Q4 2026 | `JsonRpcChannel` for polyglot guests. Host supports both legacy and new guests simultaneously. JSON-RPC contract documentation. Reference guest in Go + one other language. |
| Phase 3 | Q1 2027 | Legacy plugin interface formally deprecated. Host emits warnings for guests without embedded metadata. Plugin repository flags deprecated guests. |
| Phase 4 | Q3 2027 or later | Legacy `GobTCPChannel` and gob/net-rpc code removed. All actively maintained guests expected to have migrated. |

#### Interface Evolution Strategy

The plugin interface MUST evolve without requiring the CLI's version-switching pattern (separate `cf7`, `cf8` binaries):

1. **Backward-compatible struct evolution.** New fields added to metadata types MUST be optional (zero-valued defaults). Existing compiled guests MUST continue to work without recompilation.
2. **Additive RPC methods.** New methods MAY be added. Guests that call methods not supported by an older host SHOULD receive a clear error, not a crash.
3. **Runtime capability discovery.** Guests SHOULD be able to discover what capabilities the host provides.
4. **Deprecation signaling.** The host MUST emit runtime warnings (not errors) for deprecated methods.

### Part 4: Plugin Repository Metadata and Maintenance

#### Compatibility Metadata Format

Plugins published to the community repository SHOULD declare compatibility metadata:

```yaml
plugin:
  name: my-plugin
  version: 1.2.3
  compatibility:
    cli_plugin_interface: "v2"          # or "v3" for modernized interface
    capi: ">=3.100.0"                   # minimum CAPI version required
    cf_cli: ">=8.0.0"                   # minimum CLI version required
    v2_domain_methods: false            # true if plugin still uses V2 domain methods
```

This metadata enables:
- Users to assess whether a plugin works with their CF deployment
- The repository to flag plugins that depend on deprecated V2 methods
- Automated compatibility checks during `cf install-plugin`

#### Objective V2 Dependency Identification

The `cf-plugin-migrate scan` tool provides objective, AST-based identification of V2 dependencies. Repository maintainers MAY use scan results to populate compatibility metadata for plugins whose maintainers have not self-declared.

#### Unmaintained Plugin Policy

The policy for handling unmaintained plugins in the community repository is deferred to the CLI Working Group and TOC. This RFC provides the compatibility metadata format and scanning tools to support whatever policy is adopted.

Considerations for the policy:
- Definition of "unmaintained" (e.g., no commits in N months, no response to V2 deprecation notices)
- Whether unmaintained plugins are removed, flagged, or archived
- Process for community members to adopt abandoned plugins

## Relationship Between Parts

The four parts form a coordinated modernization program:

**Part 1 (Deprecation) enables Part 2 (Migration).** Deprecation warnings in CLI v8 create urgency for plugin developers to migrate. Part 2 provides the migration technique they need, requiring no CLI changes.

**Part 2 (Migration) bridges to Part 3 (New Interface).** The guest-side migration decouples plugins from the host's V2 code immediately. Once plugins no longer depend on V2 domain methods, the CLI team can remove legacy host code and implement the modernized interface (Part 3) on a clean foundation.

**Part 4 (Repository) supports all three.** Compatibility metadata enables the repository to communicate deprecation status (Part 1), migration status (Part 2), and interface version support (Part 3) to users.

**Independence.** Each part can proceed on its own timeline. Part 2 is available immediately. Part 1 requires a CLI release. Part 3 requires CLI team implementation work. Part 4 requires CLI WG policy decisions. No part blocks another.

## Impact and Consequences

### Positive

- **Unblocks RFC-0032.** Plugin V2 dependency is the primary obstacle to CAPI V2 removal. This RFC provides a complete path to eliminate that dependency.
- **No cross-team dependency for immediate action.** Part 2 (guest-side migration) requires no CLI team changes, no host releases, and no coordination. Plugin teams can migrate today.
- **CLI team can remove legacy code.** Once plugins migrate, the CLI team can remove `plugin/rpc/cli_rpc_server.go`, `plugin/models/`, and the `PluginModels` injection mechanism from `commandregistry`.
- **Polyglot plugin support.** JSON-RPC 2.0 and embedded metadata markers enable plugin development in any language.
- **Improved help system.** Structured flag metadata, per-plugin help, and enriched command metadata bring plugin help output in line with built-in command standards.
- **Validated approach.** Guest-side migration is validated by Rabobank production use, App Autoscaler PR #132, and OCF Scheduler proof-of-concept against live CAPI V3.

### Negative

- **go-cfclient alpha dependency.** The go-cfclient V3 library is at `v3.0.0-alpha` series. Plugin developers take on an alpha dependency. Mitigation: go-cfclient is the official CF Go client library under the `cloudfoundry` GitHub organization, and the alpha designation reflects API surface evolution, not instability.
- **CF_TRACE gap during transition.** After migration, V3 API calls made by the guest are invisible to `CF_TRACE=true` (which only traces host-side calls). Mitigation: the `cftrace.NewTracingTransport` companion package provides CF_TRACE-aware HTTP tracing for guest-side calls, injected automatically by the generated V2Compat wrapper.
- **Some V2 concepts have no V3 equivalent.** The V2 `ports` array for non-routable container ports has no V3 equivalent. V2 `IsAdmin` is always false in V3 (no admin check endpoint). Mitigation: these are edge cases affecting very few plugins; the generated wrapper documents which fields cannot be populated.
- **Two hard coupling cases.** `cf/terminal` Pattern A (multiapps, 16 files) and `cf/configuration` + `coreconfig` (cf-targets, config file I/O) have no clean drop-in replacements. These require per-plugin design decisions.

## References

### Specifications and Issues

- [RFC-0032 -- CAPI V2 Removal](https://github.com/cloudfoundry/community/blob/main/toc/rfc/rfc-0032-capi-v2-removal.md)
- [cloudfoundry/cli#3621 -- New Plugin Interface](https://github.com/cloudfoundry/cli/issues/3621)
- [community#1452 -- Original RFC PR](https://github.com/cloudfoundry/community/pull/1452)

### Libraries and Tools

- [go-cfclient -- Cloud Foundry V3 Go client library](https://github.com/cloudfoundry/go-cfclient)
- [CAPI V3 API Documentation](https://v3-apidocs.cloudfoundry.org)
- [Semantic Versioning 2.0.0](https://semver.org/)
- [JSON-RPC 2.0 Specification](https://www.jsonrpc.org/specification)

### Validated Migrations

- [Rabobank cf-plugins](https://github.com/rabobank/cf-plugins) -- production V2-to-V3 compatibility library (since 2025)
- [App Autoscaler PR #132](https://github.com/cloudfoundry/app-autoscaler-cli-plugin/pull/132) -- removed `GetApp()`, replaced with go-cfclient V3
- [cf-plugin-migrate tool](cf-plugin-migrate-design.md) -- AST-based scanner and code generator, all phases complete

### Plugin Survey

- [Plugin Interface Survey](plugin-survey.md) -- 20-plugin analysis of interface usage, internal coupling, and migration patterns

### CF CLI Guides

- [CF CLI Help Guidelines](https://github.com/cloudfoundry/cli/wiki/CF-CLI-Help-Guidelines)
- [CF CLI Style Guide](https://github.com/cloudfoundry/cli/wiki/CF-CLI-Style-Guide)
- [CLI Product-Specific Style Guide](https://github.com/cloudfoundry/cli/wiki/CLI-Product-Specific-Style-Guide)
- [Version Switching Guide](https://github.com/cloudfoundry/cli/wiki/Version-Switching-Guide)

### Detailed Design Documents

- [Transitional Migration Guide (detailed)](rfc-draft-plugin-transitional-migration-detailed.md) -- V2Compat wrapper architecture, worked examples, field mappings
- [Modernized Plugin Interface (detailed)](rfc-draft-cli-plugin-interface-v3-detailed.md) -- channel abstraction, JSON-RPC protocol, help system analysis
- [cf-plugin-migrate Design Decisions](cf-plugin-migrate-design.md) -- code generation approach, dependency chains, implementation phases

---

## Appendix: Presenting This RFC as Two Alternatives

This document is written as a single unified RFC covering the full scope of CLI plugin modernization. For the CLI WG meeting on 2026-03-25, two presentation approaches are available:

### Option A: Single RFC (This Document)

Present this document as-is. One RFC, four parts, one review process. The wider scope requested by TOC and community reviewers is satisfied in a single document.

**Advantages:**
- Single review process -- reviewers see the complete picture
- Parts are clearly connected with explicit dependency documentation
- No risk of partial acceptance creating orphaned parts
- Easier for new readers to understand the full modernization story

**Disadvantages:**
- Larger document to review
- Different parts may have different readiness levels
- CLI team changes (Parts 1 and 3) and community changes (Parts 2 and 4) are bundled

### Option B: Split into Two RFCs

Split this document at the Part 2/Part 3 boundary:

**RFC A: Plugin V2 Deprecation and Guest-Side Migration (Parts 1 + 2)**
- Deprecation timeline for V2 domain methods
- Guest-side migration technique (no CLI changes)
- Companion packages for internal import decoupling
- Can proceed immediately -- no CLI team implementation work required

**RFC B: Modernized Plugin Interface and Repository (Parts 3 + 4)**
- New PluginContext contract
- Channel abstraction and JSON-RPC
- Embedded metadata marker
- Repository compatibility metadata
- Requires CLI team implementation work -- longer timeline

**Advantages:**
- RFC A can be accepted and acted on immediately
- RFC B can take more time for design review
- Separates "what plugin developers do now" from "what the CLI team builds later"

**Disadvantages:**
- Two review processes
- Cross-references between RFCs add complexity
- Risk of RFC A being accepted without RFC B, leaving the long-term story incomplete

The working group SHOULD decide which approach best serves the community's review process.
