# Meta
[meta]: #meta
- Name: CLI Plugin Interface V3
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
7. **Insufficient versioning.** The `VersionType` struct provides only `Major`, `Minor`, and `Build` integer fields — no support for SemVer prerelease identifiers (e.g., `-rc.1`) or build metadata (e.g., `+linux.amd64`). The field name `Build` is misleading (it corresponds to SemVer's "patch" number, not build metadata). Plugins that track prerelease or build information (e.g., ocf-scheduler-cf-plugin, cf-targets-plugin) are forced to work around this by printing the full version string when invoked directly without arguments — information that is invisible to the CLI and `cf plugins`.
8. **No language portability.** The current design does not provide a maintainable path to support plugin development in languages other than Go.

### Ecosystem Issues

9. **Plugin repository decay.** The public community CLI plugin repository has become unreliable, with many published plugins unmaintained and lacking information for users to assess safety.
10. **Independent migration burden.** Plugin developers have had to independently discover how to update their plugins for CLI V7, V8, and V9 compatibility without guidance.
11. **Inconsistent UX.** Each plugin implements its own option parsing, leading to inconsistencies between plugins and increased maintenance overhead. The [CF CLI Style Guide](https://github.com/cloudfoundry/cli/wiki/CF-CLI-Style-Guide) and [Product-Specific Style Guide](https://github.com/cloudfoundry/cli/wiki/CLI-Product-Specific-Style-Guide) establish conventions for command naming (VERB-NOUN), fail-fast validation order, output formatting, color usage, error message patterns, and flag design (enum-style flags with values over boolean flags). Plugins cannot fully comply with these conventions because the plugin interface lacks the necessary metadata fields and the CLI provides no shared framework for output formatting, confirmation prompts, or error display.

### Help System Limitations

12. **No per-plugin help.** `cf help <plugin-name>` does not work — the CLI only resolves command names and aliases. Users must run `cf plugins` to discover which plugin provides which command.
13. **Plugin commands are not grouped by plugin.** In `cf help -a`, all plugin commands are listed in a single flat alphabetical list under "INSTALLED PLUGIN COMMANDS:" with no indication of their source plugin.
14. **Limited help metadata.** The plugin `Command` struct supports only a single-line `HelpText` and a flag-name-to-description map. The [CF CLI Help Guidelines](https://github.com/cloudfoundry/cli/wiki/CF-CLI-Help-Guidelines) define a standardized help format with NAME, USAGE (following [docopt](http://docopt.org/) conventions), WARNING, EXAMPLE, TIP, ALIAS, OPTIONS, and SEE ALSO sections. Built-in commands implement all of these through Go struct tags. Plugins can only provide NAME, USAGE, ALIAS, and a limited OPTIONS — they cannot provide EXAMPLE, WARNING, TIP, or SEE ALSO sections.
15. **Minimal flag metadata.** `UsageDetails.Options` is `map[string]string` — an unordered hash that loses flag display order. The CLI sorts keys alphabetically and classifies flags by key length: single-character keys become short flags (`-f`), everything else becomes a long flag (`--force`). There is no way to declare `-f` and `--force` as the same flag — each map entry becomes a separate flag with only `Short` or `Long` populated, never both. Plugins cannot specify default values, argument types, required status, or group related flags together. The [CF CLI Help Guidelines](https://github.com/cloudfoundry/cli/wiki/CF-CLI-Help-Guidelines) specify that OPTIONS should list the long option first with aliases comma-separated and defaults appended as `(Default: value)` — none of which is achievable through the `map[string]string` API. The ocf-scheduler plugin works around this entirely by embedding all flag documentation directly in the `Usage` string, bypassing the `Options` map.

### Evidence from Active Plugin Maintainers

A survey of six actively maintained CF CLI plugins reveals that the community has already organically converged on the minimal integration pattern this RFC formalizes. Every plugin uses the CLI primarily as an identity and context provider, not as a domain proxy.

#### Plugin Interface Method Usage Across Surveyed Plugins

| Method | OCF Scheduler | App Autoscaler | MultiApps (MTA) | cf-java | cf-targets | Rabobank plugins |
|---|:---:|:---:|:---:|:---:|:---:|:---:|
| `AccessToken()` | Yes | Yes | Yes | — | — | Yes |
| `ApiEndpoint()` | Yes | Yes | Yes | — | — | Yes |
| `IsSSLDisabled()` | — | Yes | Yes | — | — | Yes |
| `IsLoggedIn()` | Yes | Yes | — | — | — | Yes |
| `GetCurrentOrg()` | Yes | — | Yes | — | — | Yes |
| `GetCurrentSpace()` | Yes | Yes | Yes | — | — | Yes |
| `HasOrganization()` | — | — | — | — | — | Yes |
| `HasSpace()` | — | Yes | — | — | — | Yes |
| `Username()` | Yes | — | Yes | — | — | Yes |
| `GetApp()` / `GetApps()` | Yes | **Removed** | — | — | — | — |
| `CliCommand()` | — | — | help only | **Removed** | — | — |
| Direct CAPI V3 | — | go-cfclient | raw HTTP | `cf curl` | — | go-cfclient |
| Direct file I/O | — | — | — | — | config.json | — |

#### Per-Plugin Findings

- The **MTA CF CLI plugin** ([cloudfoundry/multiapps-cli-plugin](https://github.com/cloudfoundry/multiapps-cli-plugin)) uses the CLI for access token, API endpoint, SSL policy, current org/space, and username. All domain operations — apps, services, routes, service bindings — go through direct CAPI V3 REST calls with hand-built HTTP requests. It implements its own JWT-based token caching because `AccessToken()` can be expensive. The only uses of `CliCommand()` are displaying help text and detecting the CLI version — not for domain operations.

- The **App Autoscaler plugin** ([cloudfoundry/app-autoscaler-cli-plugin](https://github.com/cloudfoundry/app-autoscaler-cli-plugin)) explicitly migrated away from V2 methods in [PR #132](https://github.com/cloudfoundry/app-autoscaler-cli-plugin/pull/132). It removed `GetApp()` (which calls `/v2/apps`) and replaced it with `go-cfclient/v3` direct calls. It defines a custom `Connection` interface with only 6 methods: `ApiEndpoint`, `HasSpace`, `IsLoggedIn`, `AccessToken`, `GetCurrentSpace`, and `IsSSLDisabled`. A follow-up fix was needed because `IsSSLDisabled()` was not correctly forwarded to the V3 client — evidence that plugins need first-class SSL config propagation.

- The **OCF Scheduler plugin** ([ocf-scheduler-cf-plugin](https://github.com/cloudfoundry-community/ocf-scheduler-cf-plugin)) uses the CLI for login verification, access token, API endpoint, current org/space, user email, and app lookups. All scheduler API calls are made directly via HTTP. It derives the scheduler service URL from the CF API endpoint by hostname substitution — a fragile pattern that several other plugins also employ.

- The **cf-java-plugin** ([SAP/cf-cli-java-plugin](https://github.com/SAP/cf-cli-java-plugin)) progressively abandoned the plugin interface entirely. It originally used `CliConnection.CliCommand()` for `cf ssh`, but encountered authentication failures where `cf ssh` via the plugin API would fail even though `cf ssh` worked directly from the terminal. As of v4.0.2, the `cliConnection` parameter is completely ignored (`_`), and all CF interaction goes through `exec.Command("cf", ...)`. The plugin uses `cf curl /v3/apps/{GUID}/env` and `cf curl /v3/apps/{GUID}/ssh_enabled` for CAPI V3 access. It also uses `github.com/simonleung8/flags` (last updated July 2017) for option parsing, illustrating the ecosystem stagnation.

- The **cf-targets-plugin** ([cloudfoundry-community/cf-targets-plugin](https://github.com/cloudfoundry-community/cf-targets-plugin)) never calls any `CliConnection` methods at all. Instead, it directly reads and writes `~/.cf/config.json` using internal CF CLI packages (`cf/configuration`, `cf/configuration/coreconfig`). This creates a massive transitive dependency chain (Google Cloud SDK, AWS SDK, BOSH CLI, Kubernetes client-go) for a plugin that only copies JSON files. This demonstrates a gap in the plugin API: there is no way for a plugin to save/restore CLI configuration, so the plugin had to bypass the interface entirely.

- The **Rabobank CF plugins** ([rabobank/cf-plugins](https://github.com/rabobank/cf-plugins)) created a compatibility library that reimplements all V2 plugin methods (`GetApp`, `GetApps`, `GetOrgs`, `GetSpaces`, `GetServices`, etc.) using `go-cfclient/v3`. However, their own consumer plugins (scheduler, credhub, idb, npsb) barely use these reimplemented methods — they primarily use only `AccessToken()`, `ApiEndpoint()`, `GetCurrentOrg()`, `GetCurrentSpace()`, and `Username()`. The `GetApp()` reimplementation alone requires 11 separate V3 API calls to reconstruct the V2-shaped model, and the library's README warns this is "quite inefficient." This is definitive evidence that maintaining V2-shaped domain methods is unsustainable.

#### Key Observations

1. **`AccessToken()`, `ApiEndpoint()`, and `GetCurrentSpace()` are universal.** Every plugin that uses the plugin API at all uses these three methods.
2. **Domain methods are being actively removed.** The App Autoscaler plugin removed `GetApp()` in PR #132. The cf-java-plugin removed all `CliCommand()` usage. No plugin surveyed relies on `GetOrgs()`, `GetSpaces()`, `GetServices()`, or `GetRoutes()`.
3. **`CliCommand()` is unreliable.** The cf-java-plugin found that `cf ssh` via the plugin API fails where the direct CLI succeeds. The MTA plugin uses `CliCommand()` only for displaying help and detecting the CLI version — never for domain operations.
4. **Plugins duplicate boilerplate.** Every plugin independently implements the same precheck flow: verify logged in, verify org/space targeted, get token, get endpoint. This pattern appears verbatim in the OCF Scheduler, Rabobank scheduler, and Rabobank npsb plugins.
5. **The V2-to-V3 translation cost is prohibitive.** Rabobank's compatibility library proves that reconstructing V2 models from V3 APIs requires many additional API calls and produces incomplete results (e.g., `IsAdmin` is always false). It is not a viable long-term path.

## Proposal

### Design Principles

1. **CLI as context provider, not domain proxy.** The CLI MUST provide authentication, endpoint, and target context. It MUST NOT provide CF domain models or proxy CAPI endpoints.
2. **Plugin as CAPI consumer.** Plugins MUST own their CAPI V3 interaction, domain logic, and resource mapping. Plugins choose their own client libraries and communication protocols for backend service interaction — the CLI MUST NOT constrain these choices.
3. **Minimal stable contract.** The plugin API surface MUST be kept small to minimize breaking changes as the CLI evolves. The core contract MUST contain only serializable primitives (strings, booleans, simple structs) that can cross a process boundary over any wire protocol.
4. **Protocol-agnostic communication.** The CLI-to-plugin communication MUST be abstracted behind a channel interface (`Send`/`Receive`/`Open`/`Close`). The CLI code MUST NOT depend on any specific wire protocol. This enables the same contract to be served over the existing `net/rpc` transport (for backward compatibility) and JSON-RPC 2.0 (for polyglot plugin support) without changing the CLI's core logic.
5. **Language portability.** The plugin interface MUST NOT require plugins to be written in Go. Plugin metadata MUST be discoverable without executing the plugin through a Go-specific protocol. Companion packages (e.g., a Go helper that provides a pre-configured `go-cfclient` client) MAY be provided as conveniences but MUST NOT be part of the core contract.
6. **Backward-compatible transition.** The new interface SHOULD be introduced alongside the existing interface with a documented migration path and deprecation timeline.
7. **Style guide conformance.** The plugin metadata and help system SHOULD enable plugins to produce output consistent with the [CF CLI Style Guide](https://github.com/cloudfoundry/cli/wiki/CF-CLI-Style-Guide), [Help Guidelines](https://github.com/cloudfoundry/cli/wiki/CF-CLI-Help-Guidelines), and [Product-Specific Style Guide](https://github.com/cloudfoundry/cli/wiki/CLI-Product-Specific-Style-Guide). Specifically, plugins SHOULD be able to declare structured flag metadata (long/short pairs, defaults, grouping), provide EXAMPLE and SEE ALSO help sections, and follow USAGE synopsis conventions per [docopt](http://docopt.org/).

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
    Patch int

    // Pre-release and build metadata per semver 2.0
    PreRelease string   // e.g., "rc.1", "beta.2"
    BuildMeta  string   // e.g., "linux.amd64", "20260301"
}

// String returns the full SemVer 2.0 string representation.
// Examples: "1.2.3", "1.2.3-rc.1", "1.2.3+linux.amd64", "1.2.3-rc.1+linux.amd64"
func (v PluginVersion) String() string

type Command struct {
    Name         string
    Alias        string
    HelpText     string     // Short one-line description
    Description  string     // Long-form description (optional)
    Warning      string     // Critical alerts about command behavior (optional)
    Examples     string     // Usage examples (optional)
    Tip          string     // Helpful context or deprecation notices (optional)
    RelatedCmds  []string   // "See also" commands (optional)
    UsageDetails Usage
}

type Usage struct {
    Usage   string
    Options map[string]string       // Legacy: simple name → description (unordered)
    Flags   []FlagDefinition        // Preferred: structured, ordered flag metadata
}

type FlagDefinition struct {
    Long        string   // Long flag name (e.g., "output")
    Short       string   // Short flag name (e.g., "o")
    Description string
    Default     string   // Default value (e.g., "json")
    HasArg      bool     // Whether the flag takes an argument
    Required    bool     // Whether the flag is required
    Group       string   // Optional group header (e.g., "Output options")
}
```

When `Flags` is populated, the CLI MUST use it for help display instead of `Options`. When only `Options` is populated, the current behavior is preserved. This maintains full backward compatibility while allowing plugins to declare paired long/short flags (`--force`/`-f`), specify defaults and required status, and organize flags into logical groups.

### Methods Explicitly Removed from the Plugin API

The following categories of methods from the current plugin interface MUST NOT be carried forward, as they embed CF domain models into the plugin contract:

- **Application methods:** `GetApp`, `GetApps` — plugins MUST use CAPI V3 directly
- **Service methods:** `GetServices`, `GetService`, `GetServiceInstances` — plugins MUST use CAPI V3 directly
- **Organization methods:** `GetOrg`, `GetOrgs` — plugins MUST use CAPI V3 to query beyond current context
- **Space methods:** `GetSpace`, `GetSpaces` — plugins MUST use CAPI V3 to query beyond current context
- **Route methods:** `GetRoutes` — plugins MUST use CAPI V3 directly
- **CLI command execution:** `CliCommand`, `CliCommandWithoutTerminalOutput` — plugins MUST NOT depend on CLI command output parsing

### CF Client Access (Companion Package)

`CfClient()` MUST NOT be part of the core plugin contract. The core contract provides only serializable primitives (`AccessToken()`, `ApiEndpoint()`, `IsSSLDisabled()`) that cross the process boundary between the CLI and plugin. A pre-configured CF client object cannot be serialized over any wire protocol (net/rpc, JSON-RPC, gRPC, or otherwise).

Instead, a companion Go package SHOULD be provided (e.g., `code.cloudfoundry.org/cli-plugin-helpers/cfclient`) that constructs a [go-cfclient](https://github.com/cloudfoundry/go-cfclient) V3 client from the core contract primitives:

```go
// Package cfhelper — companion package, NOT part of the core contract.
package cfhelper

import (
    "github.com/cloudfoundry/go-cfclient/v3/client"
    "github.com/cloudfoundry/go-cfclient/v3/config"
)

// NewCfClient creates a go-cfclient V3 client using session information
// from the plugin context. This is the RECOMMENDED way for Go plugins
// to interact with Cloud Foundry.
func NewCfClient(ctx PluginContext) (*client.Client, error) {
    token, err := ctx.AccessToken()
    if err != nil {
        return nil, err
    }
    endpoint, err := ctx.ApiEndpoint()
    if err != nil {
        return nil, err
    }
    skipSSL, err := ctx.IsSSLDisabled()
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

This design reflects a three-layer architecture:

```
Layer 1: CLI ←→ Plugin hosting (channel abstraction: Send/Receive/Open/Close)
Layer 2: Core contract (serializable primitives: tokens, endpoints, context)
Layer 3: Plugin ←→ Backend service (plugin's choice: go-cfclient, gRPC, HTTP, etc.)
```

The core contract (Layer 2) provides the building blocks. The companion package is a Layer 3 convenience for Go plugins. Plugins in other languages use their own CF client libraries — a Python plugin would use its own HTTP library, a Java plugin would use cf-java-client — all using the same `AccessToken()` and `ApiEndpoint()` primitives from the core contract.

This separation:
- Keeps the core contract dependency-free and language-agnostic
- Avoids coupling the interface module version to go-cfclient's version (which is still at alpha)
- Allows the companion package to evolve independently
- Enables polyglot plugins without any core contract changes

### Additional Endpoint Access

The plugin interface SHOULD provide methods to discover related CF platform service endpoints:

| Method | Return Type | Description |
|---|---|---|
| `UaaEndpoint()` | `(string, error)` | UAA server URL for direct token operations |
| `LoggregatorEndpoint()` | `(string, error)` | Log aggregator endpoint |
| `DopplerEndpoint()` | `(string, error)` | Doppler WebSocket endpoint |
| `RoutingApiEndpoint()` | `(string, error)` | Routing API endpoint |

These endpoints enable plugins to integrate with platform services beyond the Cloud Controller without having to discover endpoints independently.

### Communication Architecture

#### Current State

The current CLI-to-plugin communication uses Go's `net/rpc` with `encoding/gob` serialization over TCP on localhost. The CLI starts an RPC server on a random port, launches the plugin as a subprocess, and passes the port as `os.Args[1]`. The plugin dials `127.0.0.1:<port>` to call back into the CLI for context methods (`AccessToken()`, `GetCurrentSpace()`, etc.). This architecture has two limitations: `encoding/gob` is Go-specific (preventing polyglot plugins), and each RPC call opens a new TCP connection (inefficient for plugins that make many context calls).

#### Channel Abstraction

The CLI MUST abstract all plugin communication behind a channel interface:

```go
type PluginChannel interface {
    Open() error
    Send(msg Message) error
    Receive() (Message, error)
    Close() error
}
```

The CLI's core logic — command dispatch, metadata retrieval, context serving — works only with `PluginChannel`. The channel implementation handles transport and serialization. This allows the same CLI code to serve plugins over different protocols:

| Implementation | Transport | Serialization | Use Case |
|---|---|---|---|
| `GobTCPChannel` | TCP localhost | `encoding/gob` | Legacy Go plugins (backward compat) |
| `JsonRpcChannel` | TCP localhost | JSON-RPC 2.0 | New polyglot plugins |

The legacy `net/rpc` transport does not need to be replaced — it is perfectly serviceable plumbing. It can carry new JSON-RPC payloads alongside the existing gob-encoded methods. The transport is not the constraint; the serialization format is.

#### Message Format: JSON-RPC 2.0

New plugins MUST use [JSON-RPC 2.0](https://www.jsonrpc.org/specification) as the message format. JSON-RPC provides:
- **Bidirectional request/response** — both CLI and plugin can initiate calls, with correlation IDs for matching responses
- **Notifications** — one-way messages (e.g., progress updates, cancellation signals)
- **Standardized error codes** — structured error reporting
- **Universal language support** — any language that can read/write JSON can participate

The CLI and plugin communicate bidirectionally over the same channel:

```
CLI                                         Plugin
 │                                            │
 │  {"jsonrpc":"2.0","method":"Run",          │
 │   "params":{"command":"create-job",        │
 │             "args":["myapp","myjob"]},     │
 │   "id":1}                                  │
 │───────────────────────────────────────────►│
 │                                            │
 │  {"jsonrpc":"2.0","method":"AccessToken",  │
 │   "id":100}                                │
 │◄───────────────────────────────────────────│
 │                                            │
 │  {"jsonrpc":"2.0","result":"bearer eyJ..", │
 │   "id":100}                                │
 │───────────────────────────────────────────►│
 │                                            │
 │  {"jsonrpc":"2.0","result":{"status":"ok"},│
 │   "id":1}                                  │
 │◄───────────────────────────────────────────│
```

#### Output Separation

`stdout` and `stderr` MUST remain available for the plugin's user-facing output. The JSON-RPC protocol channel MUST use a separate transport (TCP connection). This preserves the current behavior where plugins write directly to `stdout` for user output, while keeping the protocol stream clean and parseable.

#### Install-Time Metadata: Embedded Marker

During `cf install-plugin`, the CLI needs to retrieve plugin metadata without knowing what protocol the plugin speaks. The CLI MUST NOT need to execute the plugin to discover its metadata.

Plugins MUST embed a `CF_PLUGIN_METADATA:` marker string followed by a JSON object in the plugin file (binary or script). The CLI scans the file for this marker and extracts the metadata directly:

**Compiled Go binary:**
```go
var _ = `CF_PLUGIN_METADATA:{"name":"AutoScaler","protocol":"jsonrpc","version":{"major":4,"minor":1,"patch":2},"commands":[{"name":"autoscaling-api","alias":"asa","help_text":"Set or view AutoScaler service API endpoint"}]}`
```

**Python script:**
```python
#!/usr/bin/env python3
# CF_PLUGIN_METADATA:{"name":"my-plugin","protocol":"jsonrpc","version":{"major":1,"minor":0,"patch":0},"commands":[{"name":"my-command","help_text":"Does something"}]}
```

**Perl script:**
```perl
#!/usr/bin/env perl
# CF_PLUGIN_METADATA:{"name":"my-plugin","protocol":"jsonrpc","version":{"major":1,"minor":0,"patch":0},"commands":[{"name":"my-command","help_text":"Does something"}]}
```

The install flow becomes:

```
1. Read plugin file (binary or script)
2. Scan for CF_PLUGIN_METADATA: marker
3. Found → parse JSON, store metadata + protocol in config.json, copy to plugins dir
4. Not found → legacy Go plugin, use existing exec + gob/net/rpc metadata exchange
```

This approach:
- Works with any language (compiled or interpreted) — any executable can contain the marker string
- Requires no execution of untrusted code during install
- Is deterministic — no timeout-based protocol probing or fallback heuristics
- Preserves full backward compatibility with existing Go plugins (no marker = legacy)
- Includes a `protocol` field so the CLI knows how to communicate at runtime

The embedded metadata JSON MUST include a `schema_version` field to allow the format to evolve:

```json
{"schema_version":1,"name":"...","protocol":"jsonrpc","version":{...},"commands":[...]}
```

#### Runtime Protocol Selection

At runtime, the CLI reads the stored `protocol` field from plugin config and selects the appropriate channel implementation:

```go
plugin := config.GetPlugin("AutoScaler")

var ch PluginChannel
switch plugin.Protocol {
case "jsonrpc":
    ch = NewJsonRpcChannel(listener)
case "netrpc-gob", "":
    ch = NewGobChannel(listener)  // legacy default
}

ch.Open()
defer ch.Close()
ch.Send(RunCommand{Name: command, Args: args})
// ... handle bidirectional communication
```

Connection information (e.g., TCP port) SHOULD be passed to new-protocol plugins via environment variables (`CF_PLUGIN_PORT`, `CF_PLUGIN_PROTOCOL`) rather than positional arguments, to avoid conflicts with plugin argument parsing. Legacy plugins continue to receive the port as `os.Args[1]`.

#### Security Improvement

The channel abstraction also improves security. The current model exposes a TCP port on localhost that is accessible to any process on the machine during the plugin's execution window. Future channel implementations MAY use process-private communication mechanisms (e.g., file descriptor passing) to eliminate this exposure.

### Enhanced Plugin Metadata

#### Semantic Versioning

Plugin versions MUST follow [Semantic Versioning 2.0.0](https://semver.org/). The current `VersionType` struct uses only three integer fields (`Major`, `Minor`, `Build`) with no prerelease or build metadata support. The `Build` field name is a misnomer — it corresponds to SemVer's "patch" number, not build metadata. Plugins that need to communicate prerelease status (e.g., `1.0.0-rc.1`) or platform-specific build identifiers (e.g., `+linux.amd64`) cannot do so through the plugin API. The ocf-scheduler, app-autoscaler, and cf-targets plugins work around this by printing the full version string when invoked directly without arguments — but this information is invisible to `cf plugins`.

Additionally, plugins that front a backend service have two versions to track: the plugin's own release version and the version of the service API they interface with. For example, the App Autoscaler plugin (v4.x) talks to the autoscaler service's `/v1/` API, and the OCF Scheduler plugin has its own SemVer but depends on the Scheduler service API. These versions are independently maintained, yet the current `VersionType` provides no way to express service API compatibility — plugins cannot signal which service API version they require or support.

The new `PluginVersion` struct renames `Build` to `Patch` for clarity and adds `PreRelease` and `BuildMeta` string fields for full SemVer 2.0 compliance. The CLI MUST display the full version string (including prerelease and build metadata when present) in `cf plugins` output and SHOULD warn users when a plugin's `MinCliVersion` exceeds the current CLI version.

#### Improved Help Integration

##### Current State

The current help system has several limitations identified through code analysis of the CLI's `command/common/help_command.go` and related files:

1. **No per-plugin help.** `cf help <plugin-name>` does not work. The help system only resolves command names and aliases, not plugin names. Users must run `cf plugins` to see which plugin provides which command.

2. **Plugin commands are not grouped by plugin.** In `cf help -a`, all plugin commands appear in a single flat list under "INSTALLED PLUGIN COMMANDS:" sorted alphabetically. There is no indication of which plugin provides which command.

3. **Limited metadata in help output.** The current `Command` struct supports only `Name`, `Alias`, `HelpText` (single-line), and `UsageDetails` (usage string + flag name-to-description map). Built-in commands display examples, related commands, and environment variables through Go struct tags — capabilities unavailable to plugins.

4. **Minimal flag metadata.** `UsageDetails.Options` is `map[string]string` — flag name to description only. There is no way for plugins to specify default values, whether a flag takes an argument, or whether it is required.

5. **`cf help` common view shows no descriptions.** Plugin commands appear as a 3-column table of names and aliases only — the `HelpText` is not shown. Users must run `cf help -a` or `cf help <command>` to see descriptions.

##### Proposed Improvements

The CLI SHOULD implement the following improvements:

**1. Per-plugin help: `cf help <plugin-name>`**

When a user runs `cf help <plugin-name>`, the CLI SHOULD display all commands from that plugin:

```
PLUGIN:
   OCFScheduler v1.2.3

COMMANDS:
   create-job             Create a job                     [Aliases: cj]
   delete-job             Delete a job                     [Aliases: dj]
   schedule-job           Schedule a job
   jobs                   List all jobs

Use 'cf help <command>' for details on a specific command.
```

This requires modifying the CLI's `findPlugin()` method to also match against `PluginMetadata.Name`, not just command names and aliases.

**2. Group plugin commands by plugin in `cf help -a`**

Instead of a flat list, the CLI SHOULD group commands by their providing plugin:

```
INSTALLED PLUGIN COMMANDS:
  OCFScheduler v1.2.3:
     create-job           Create a job
     jobs                 List all jobs
     schedule-job         Schedule a job

  AutoScaler v4.1.1:
     autoscaling-api      Set or view AutoScaler API endpoint
     autoscaling-policy   Retrieve the scaling policy of an app
```

**3. Enriched `Command` struct**

The `Command` struct SHOULD be extended with optional fields for richer help:

```go
type Command struct {
    Name         string
    Alias        string
    HelpText     string     // Short one-line description (existing)
    Description  string     // Long-form description (new, optional)
    Warning      string     // Critical alerts about command behavior (new, optional)
    Examples     string     // Usage examples (new, optional)
    Tip          string     // Helpful context or deprecation notices (new, optional)
    RelatedCmds  []string   // "See also" commands (new, optional)
    UsageDetails Usage
}
```

These fields align with the [CF CLI Help Guidelines](https://github.com/cloudfoundry/cli/wiki/CF-CLI-Help-Guidelines) standard help sections: NAME, USAGE, WARNING, EXAMPLE, TIP, ALIAS, OPTIONS, SEE ALSO. When a plugin provides these optional fields, `cf help <command>` SHOULD display them in the corresponding sections, matching the format used for built-in commands. Existing plugins that do not set them continue to work without changes.

**4. Structured flag metadata with grouping**

The `Usage` struct's `Flags []FlagDefinition` field (defined above in [Plugin Registration](#4-plugin-registration)) replaces the legacy `Options map[string]string`. Key improvements:

- **Ordered display.** Flags render in the order declared, not alphabetically by hash key.
- **Paired long/short names.** A single `FlagDefinition` with `Long: "force", Short: "f"` renders as `--force, -f` — impossible with the current map-based approach where each key produces a separate, unpaired entry.
- **Defaults and required markers.** `Default` and `Required` fields enable `cf help <command>` to display `(Default: json)` or `[required]` annotations, matching the built-in command style.
- **Flag grouping.** The `Group` field allows plugins to organize flags under logical headers (e.g., "Output options", "Authentication"), producing output like:

```
OPTIONS:
   Output options:
      --format, -f          Output format (Default: table)
      --output, -o          Write output to file

   Filtering:
      --label, -l           Filter by label selector
      --limit                Maximum number of results
```

If `Flags` is populated, the CLI MUST use it for help display instead of `Options`. If only `Options` is populated, the current behavior is preserved. This maintains full backward compatibility.

### Plugin Repository Improvements

While a full plugin repository redesign is outside the scope of this RFC, the following requirements inform future work:

1. Plugins SHOULD declare their minimum compatible CLI version.
2. Plugins SHOULD declare their minimum compatible CAPI version.
3. The plugin repository SHOULD implement a deprecation process for plugins that have not been updated within a defined period.
4. The plugin repository SHOULD provide build and dependency metadata to help users assess plugin safety.

### Migration Path

#### Phase 1: Channel Abstraction and Embedded Metadata (Target: Q3 2026)

- Implement the `PluginChannel` interface (`Send`/`Receive`/`Open`/`Close`) in the CLI.
- Implement `GobTCPChannel` wrapping the existing `net/rpc` transport — no behavior change for existing plugins.
- Add `CF_PLUGIN_METADATA:` marker scanning to `cf install-plugin`.
- Publish the core contract as a standalone Go module (e.g., `code.cloudfoundry.org/cli-plugin-api/v2`) with zero external dependencies.
- Publish the `cfhelper` companion package for Go plugins.
- Document migration guides with before/after examples.

#### Phase 2: JSON-RPC and Polyglot Support (Target: Q4 2026)

- Implement `JsonRpcChannel` for new-protocol plugins.
- The CLI MUST support both legacy (gob/net/rpc) and new (JSON-RPC) plugins simultaneously, determined by the `protocol` field stored during install.
- Publish JSON-RPC contract documentation (method names, parameter schemas, error codes) so that non-Go plugins can be developed.
- Provide reference plugin implementations in Go and at least one other language (e.g., Python).

#### Phase 3: Deprecation (Target: Q1 2027)

- The legacy plugin interface is formally deprecated.
- The CLI emits warnings when loading plugins that do not have embedded metadata (legacy protocol).
- Plugin repository begins flagging plugins that use the deprecated interface.

#### Phase 4: Removal (Target: Q3 2027 or later)

- The legacy `GobTCPChannel` and associated gob/net/rpc code is removed from the CLI.
- All actively maintained plugins are expected to have migrated to embedded metadata and JSON-RPC.

#### Interface Evolution Strategy

The [CF CLI Version Switching Guide](https://github.com/cloudfoundry/cli/wiki/Version-Switching-Guide) describes the CLI's current approach to major version changes: separate binaries (`cf7`, `cf8`) with symlink routing, requiring full uninstall/reinstall on some platforms. The plugin interface MUST NOT require this approach for its own evolution. Instead:

1. **Backward-compatible struct evolution.** New fields added to `PluginMetadata`, `Command`, `Usage`, `PluginVersion`, and `FlagDefinition` MUST be optional (zero-valued defaults). Existing compiled plugins MUST continue to work without recompilation.
2. **Additive RPC methods.** New methods MAY be added to the RPC interface. Plugins that call methods not supported by an older CLI SHOULD receive a clear error indicating the method is unavailable, rather than a crash.
3. **Runtime capability discovery.** Plugins SHOULD be able to discover what capabilities the host CLI provides, enabling graceful degradation on older CLIs (e.g., check whether `RefreshToken()` is available, fall back to single-use `AccessToken()` if not).
4. **Deprecation signaling.** When the CLI deprecates plugin API methods, it MUST emit runtime warnings (not errors) so that plugin users know to request updates from plugin maintainers.

### Reference Architecture

The following diagram illustrates the three-layer plugin architecture:

```
┌──────────────────────────────────────────────────────────┐
│                         CF CLI                            │
│                                                          │
│  ┌────────────────────────────────────────────────────┐  │
│  │  Layer 2: Core Contract (serializable primitives)  │  │
│  │                                                    │  │
│  │  Session:  AccessToken, RefreshToken,              │  │
│  │           IsLoggedIn, Username                     │  │
│  │                                                    │  │
│  │  Endpoint: ApiEndpoint, IsSSLDisabled,             │  │
│  │           ApiVersion, UaaEndpoint                  │  │
│  │                                                    │  │
│  │  Context:  GetCurrentOrg, GetCurrentSpace,         │  │
│  │           HasOrganization, HasSpace                │  │
│  └──────────────────────┬─────────────────────────────┘  │
│                          │                                │
│  ┌──────────────────────▼─────────────────────────────┐  │
│  │  Layer 1: Channel Abstraction (Send/Receive/       │  │
│  │           Open/Close)                              │  │
│  │                                                    │  │
│  │  ┌──────────────┐  ┌───────────────────────────┐  │  │
│  │  │ GobTCPChannel│  │ JsonRpcChannel            │  │  │
│  │  │ (legacy)     │  │ (new — polyglot)          │  │  │
│  │  └──────────────┘  └───────────────────────────┘  │  │
│  └────────────────────────────────────────────────────┘  │
└──────────────────────────┬───────────────────────────────┘
                           │
                           │ TCP (localhost)
                           │
┌──────────────────────────▼───────────────────────────────┐
│                        Plugin                             │
│  (any language: Go, Python, Java, Perl, Rust, ...)       │
│                                                          │
│  ┌──────────────────┐  ┌─────────────────────────────┐  │
│  │  Embedded         │  │  Layer 3: Domain Logic       │  │
│  │  Metadata         │  │  (plugin's choice)           │  │
│  │  (CF_PLUGIN_      │  │                              │  │
│  │   METADATA:)      │  │  Go:     go-cfclient, HTTP   │  │
│  │                   │  │  Python: requests, grpcio    │  │
│  │  stdout/stderr    │  │  Java:   cf-java-client      │  │
│  │  for user output  │  │  Any:    gRPC, REST, custom  │  │
│  └──────────────────┘  └──────────────┬──────────────┘  │
└───────────────────────────────────────┼──────────────────┘
                                        │
                              Plugin's choice of protocol
                              (HTTP, gRPC, RSocket, etc.)
                                        │
                                        ▼
                         ┌──────────────────────────┐
                         │  Cloud Controller V3 API  │
                         │  Autoscaler API           │
                         │  Scheduler API            │
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
import (
    pluginapi "code.cloudfoundry.org/cli-plugin-api/v2"
    "code.cloudfoundry.org/cli-plugin-helpers/cfclient"
)

func (p *MyPlugin) Run(ctx pluginapi.PluginContext, args []string) {
    // Get a configured V3 client via companion package
    client, _ := cfhelper.NewCfClient(ctx)

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

**Enabled by this RFC but requiring further design:**

- **Polyglot plugin SDKs.** The embedded metadata marker and JSON-RPC 2.0 protocol enable plugins in any language. Community-maintained SDKs for Python, Java, and other languages can be developed independently of the CLI, implementing the JSON-RPC channel client and providing language-idiomatic wrappers for the core contract methods.

**Deferred to separate RFCs:**

- **GitHub-style plugin distribution** — trust model, signing, and automated security scanning.
- **CLI adoption of go-cfclient internally** — centralizing CAPI interaction across CLI and plugins.
- **Standard option parsing** — providing a shared flag parsing framework to improve UX consistency across plugins.

## References

### Issue and Specification

- [cloudfoundry/cli#3621 — New Plugin Interface](https://github.com/cloudfoundry/cli/issues/3621)
- [Current plugin interface — code.cloudfoundry.org/cli/plugin](https://pkg.go.dev/code.cloudfoundry.org/cli/plugin)
- [go-cfclient — Cloud Foundry V3 Go client library](https://github.com/cloudfoundry/go-cfclient)
- [Semantic Versioning 2.0.0](https://semver.org/)
- [JSON-RPC 2.0 Specification](https://www.jsonrpc.org/specification)
- [docopt — Command-line interface description language](http://docopt.org/)

### CF CLI Wiki Guides

- [CF CLI Help Guidelines](https://github.com/cloudfoundry/cli/wiki/CF-CLI-Help-Guidelines) — Standard help section format (NAME, USAGE, WARNING, EXAMPLE, TIP, ALIAS, OPTIONS, SEE ALSO)
- [CF CLI Style Guide](https://github.com/cloudfoundry/cli/wiki/CF-CLI-Style-Guide) — Command naming (VERB-NOUN), fail-fast validation, output formatting, color conventions, flag design
- [CLI Product-Specific Style Guide](https://github.com/cloudfoundry/cli/wiki/CLI-Product-Specific-Style-Guide) — Error message patterns, TIP conventions, idempotent operations, table column ordering
- [Version Switching Guide](https://github.com/cloudfoundry/cli/wiki/Version-Switching-Guide) — CLI version management via separate binaries and symlinks

### Surveyed Plugins

- [cloudfoundry/app-autoscaler-cli-plugin](https://github.com/cloudfoundry/app-autoscaler-cli-plugin) — [PR #132: Switch to V3 CF API client](https://github.com/cloudfoundry/app-autoscaler-cli-plugin/pull/132)
- [cloudfoundry/multiapps-cli-plugin](https://github.com/cloudfoundry/multiapps-cli-plugin) (MTA)
- [cloudfoundry-community/ocf-scheduler-cf-plugin](https://github.com/cloudfoundry-community/ocf-scheduler-cf-plugin)
- [SAP/cf-cli-java-plugin](https://github.com/SAP/cf-cli-java-plugin)
- [cloudfoundry-community/cf-targets-plugin](https://github.com/cloudfoundry-community/cf-targets-plugin)
- [rabobank/cf-plugins](https://github.com/rabobank/cf-plugins) — V2-to-V3 compatibility library
- [rabobank/scheduler-plugin](https://github.com/rabobank/scheduler-plugin)
- [rabobank/credhub-plugin](https://github.com/rabobank/credhub-plugin)
- [rabobank/idb-plugin](https://github.com/rabobank/idb-plugin)
- [rabobank/npsb-plugin](https://github.com/rabobank/npsb-plugin)
