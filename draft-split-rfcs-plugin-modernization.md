# CF CLI Plugin Modernization — Split RFC Approach

**Prepared for:** CLI Working Group Meeting, 2026-03-25
**Author:** @norman-abramovitz
**Approach:** Split RFC strategy (proposed by author, supported by @beyhan/TOC)

## Overview

This document contains four focused RFC drafts that together address CF CLI plugin modernization. The split approach was chosen over a single monolithic RFC because each piece addresses a distinct concern, has its own stakeholders, and can progress through review independently.

### How the RFCs Relate

```
RFC A (Strategy) ← references all three
  ├── RFC B (Migration) — can proceed immediately
  ├── RFC C (V9 Interface) — depends on RFC A direction
  └── RFC D (Repo Maintenance) — can proceed independently
```

**RFC A** sets the strategic direction: V2 plugin methods are deprecated, plugins migrate to V3, a new minimal interface ships in CLI v9, and the plugin repository gets compatibility metadata. It references the other three RFCs for implementation details.

**RFC B** is the most mature — it is essentially the existing PR #1452 with minor updates. It describes the guest-side transitional migration technique that plugin developers can adopt today, without waiting for CLI team changes.

**RFC C** defines the future plugin interface for CLI v9+. It depends on RFC A establishing the deprecation direction but can be drafted in parallel.

**RFC D** addresses plugin repository maintenance — compatibility metadata and unmaintained plugin policy. It can proceed independently of the other three.

### Alternative: beyhan's "Future Improvements" Pattern

@beyhan suggested (2026-03-20) that a dedicated overarching strategy RFC (RFC A) may not be needed. Instead, the first RFC (RFC B, PR #1452) could include a **Future Improvements** section that broadly outlines the big picture, with subsequent RFCs referencing it for context. This pattern has been used in past CFF RFCs. Under this approach, RFC A's content would be absorbed into RFC B's Future Improvements section, reducing the split to 3 RFCs (B, C, D). A "Future Improvements" section has been added to `rfc-draft-plugin-transitional-migration.md` to support this option.

### Submission Plan

Each RFC will be submitted as a separate PR to `cloudfoundry/community`. RFC B already exists as PR #1452. RFCs A (if kept separate), C, and D will be new PRs referencing each other by PR number once submitted.

---

# RFC A: CF CLI Plugin V2 Deprecation and Migration Strategy

## Meta
[meta]: #meta
- Name: CF CLI Plugin V2 Deprecation and Migration Strategy
- Start Date: 2026-03-25
- Author(s): @norman-abramovitz
- Status: Draft
- RFC Pull Request: (to be created)
- Tracking Issue: [cloudfoundry/cli#3621](https://github.com/cloudfoundry/cli/issues/3621)

## Summary

The CF CLI plugin interface exposes 10 methods that return CAPI V2-shaped data structures (`GetApp`, `GetApps`, `GetService`, `GetServices`, `GetOrg`, `GetOrgs`, `GetSpaces`, `GetOrgUsers`, `GetSpaceUsers`, `GetRoutes`). With CAPI V2 reaching end of life per [RFC-0032](https://github.com/cloudfoundry/community/blob/main/toc/rfc/rfc-0032-deprecation-of-cf-api-v2.md), these methods will become non-functional, affecting at least 18 actively maintained plugins across the Cloud Foundry ecosystem. This RFC establishes a four-part strategy: deprecate V2 plugin methods, provide a transitional migration path, define a minimal stable plugin interface for CLI v9, and introduce compatibility metadata for the plugin repository.

## Problem

The current CF CLI plugin interface has several interrelated problems that require a coordinated strategy:

1. **V2 dependency chain.** The plugin interface returns `plugin_models.*` types that mirror CAPI V2 response structures. When V2 endpoints are removed per RFC-0032, the 10 V2 domain methods will stop working. A survey of 20 community plugins found that 13 use `AccessToken()`, establishing the plugin interface as actively relied upon, while several plugins still depend on V2 domain methods.

2. **Host-side maintenance burden.** The CF CLI maintains V2 domain method RPC handlers, V2-shaped response types, and config-derived accessors solely to serve plugins. This code exists only for backward compatibility and increases maintenance cost.

3. **Plugin-to-CLI internal coupling.** 11 of 20 surveyed plugins import CLI internal packages (`cf/terminal`, `cf/trace`, `cf/configuration/confighelpers`, etc.) that were never intended as public API. Any refactoring of these packages would break those plugins without warning.

4. **No ecosystem visibility.** The plugin repository provides no way to determine which plugins work on V2-disabled foundations, and unmaintained plugins (some last updated in 2017) remain listed without qualification.

These problems span multiple concerns — deprecation policy, migration technique, interface design, and repository maintenance — which is why this RFC proposes a split approach with three companion RFCs.

## Proposal

### Strategy Overview

This RFC declares the strategic direction for CF CLI plugin modernization. The implementation details are specified in three companion RFCs:

| RFC | Scope | Status |
|-----|-------|--------|
| **RFC B** — Transitional Migration to CAPI V3 | Guest-side migration technique, cf-plugin-migrate tool, companion packages | Draft (PR #1452) |
| **RFC C** — Plugin Interface V9 Minimal Stable Contract | New plugin interface for CLI v9+, polyglot support, JSON-RPC | Draft |
| **RFC D** — Plugin Repository Compatibility and Maintenance | Compatibility metadata, unmaintained plugin policy | Draft |

### V2 Domain Method Deprecation

The following methods on `plugin.CliConnection` are declared **deprecated** as of CF CLI v8:

- `GetApp`, `GetApps`
- `GetService`, `GetServices`
- `GetOrg`, `GetOrgs`
- `GetSpaces`
- `GetOrgUsers`, `GetSpaceUsers`
- `GetRoutes`

These methods MUST continue to function in CF CLI v8 for the duration of the migration period. They MUST NOT be carried forward into the V9 plugin interface (RFC C).

`CliCommand` and `CliCommandWithoutTerminalOutput` are not deprecated in v8 but MUST NOT be carried forward into V9. Plugin developers SHOULD migrate `cf curl` usage to direct CAPI V3 calls during the transitional period.

### CLI Version Support Lines

| CLI Version | Plugin Interface | Status |
|-------------|-----------------|--------|
| v7 | Current (V2 domain methods) | Deprecated — no new features |
| v8 | Current with deprecation warnings | Current — migration period |
| v9 | V9 minimal contract (RFC C) | Future — V2 methods removed |

### Migration Period

The migration period aligns with the RFC-0032 CAPI V2 removal timeline:

| Phase | Timeframe | Actions |
|-------|-----------|---------|
| **Announce** | Immediate (Q2 2026) | Deprecation notice in CLI release notes, plugin developer outreach |
| **Migrate** | Q2–Q4 2026 | Plugin developers adopt RFC B transitional migration; cf-plugin-migrate tool available |
| **Warn** | Q1 2027 | CLI v8.x emits runtime deprecation warnings when V2 domain methods are called |
| **Remove** | Q3 2027+ | CLI v9 ships with V9 plugin interface (RFC C); V2 domain methods removed from host |

### Context Methods Retained

The following context methods MUST be retained across all CLI versions as they form the stable core of the plugin contract:

- `AccessToken`, `ApiEndpoint`, `IsSSLDisabled`, `ApiVersion`
- `IsLoggedIn`, `Username`
- `GetCurrentOrg`, `GetCurrentSpace`, `HasOrganization`, `HasSpace`
- `HasAPIEndpoint`

### Success Criteria

1. All actively maintained plugins compile and function against CLI v9 without V2 domain method calls.
2. The plugin repository includes compatibility metadata for all listed plugins.
3. At least two plugins from different organizations have completed the transitional migration (RFC B) before the warning phase begins.
4. The V9 plugin interface (RFC C) supports at least one non-Go plugin as a proof of concept.

---

# RFC B: CLI Plugin Transitional Migration to CAPI V3

## Meta
[meta]: #meta
- Name: CLI Plugin Transitional Migration to CAPI V3
- Start Date: 2026-03-01
- Author(s): @norman-abramovitz
- Status: Draft
- RFC Pull Request: [cloudfoundry/community#1452](https://github.com/cloudfoundry/community/pull/1452)
- Tracking Issue: [cloudfoundry/cli#3621](https://github.com/cloudfoundry/cli/issues/3621)

## Summary

The CF CLI plugin interface depends on CAPI V2, which is reaching end of life. When V2 endpoints are removed, plugins that rely on the host's V2 domain methods will break — affecting at least 18 actively maintained plugins. This RFC proposes a transitional migration that plugin teams can adopt today, without waiting for CLI team changes or a new plugin interface. A companion tool (`cf-plugin-migrate`) scans a plugin's source code, identifies exactly what V2 functionality it uses, and generates drop-in replacement code backed by CAPI V3. The approach is validated by the [Rabobank cf-plugins](https://github.com/rabobank/cf-plugins) library, which has been in production since 2025.

## Problem

### CAPI V2 Is Reaching End of Life

Plugins that depend on V2-shaped data from the host's domain methods (`GetApp`, `GetApps`, `GetService`, etc.) will stop working when V2 endpoints are removed. The current plugin interface returns `plugin_models.*` types that mirror V2 response structures — these types cannot represent V3 concepts like multiple process types, sidecars, rolling deployments, or metadata labels.

### The Host Carries Legacy Code for Plugin Support

The CF CLI host implements 10 V2 domain methods on behalf of plugins via RPC. Each method involves an RPC handler in `plugin/rpc/cli_rpc_server.go`, V2-shaped response types in `plugin/models/`, and config-derived accessors. This code exists solely to serve the guest-side plugin interface.

### CliCommand Is a Fragile Escape Hatch

Many plugins use `CliCommand` or `CliCommandWithoutTerminalOutput` to run arbitrary CF CLI commands, including `cf curl` for direct CAPI access. A scan of 18 actively maintained plugins found `CliCommand` usage across 14 plugins, with patterns ranging from simple `cf apps` to complex `cf curl` with JSON parsing and pagination.

### Plugins Import CLI Internal Packages

Beyond the intended public interface, 8 of 18 surveyed plugins import internal CLI packages — creating a build-time dependency on code the CLI team never intended to expose. The most common imports are `cf/terminal` (6 plugins), `cf/trace` (6 plugins), and `cf/configuration/confighelpers` (4 plugins).

## Proposal

### Guest-Side V2 Compatibility Wrapper

The migration introduces a thin wrapper on the guest side that:

- **Embeds** `plugin.CliConnection` — all context methods pass through unchanged
- **Constructs** a go-cfclient V3 `*client.Client` from `AccessToken()`, `ApiEndpoint()`, and `IsSSLDisabled()`
- **Reimplements** only the V2 domain methods the plugin actually uses, backed by the minimum V3 API calls required
- **Satisfies** `plugin.CliConnection` — existing code that accepts the connection interface works without changes

### The cf-plugin-migrate Tool

`cf-plugin-migrate` is an AST-based audit and code generation tool:

1. **`scan`** — Analyzes a plugin's Go source to produce a complete inventory of V2 interface usage: which V2 domain methods are called, which fields on the returned models are accessed, which `CliCommand`/`cf curl` patterns are used, and which internal CLI packages are imported.

2. **`generate`** — Produces a drop-in `V2Compat` wrapper implementing only the V2 methods the plugin actually calls, backed by go-cfclient V3.

### Two Migration Paths

| Path | When to Use | Effort |
|------|-------------|--------|
| **Generated wrapper** | Plugin uses V2 domain methods, wants minimal code changes | Low — run scanner, generate wrapper, update `Run()` entry point |
| **Direct V3** | Plugin is ready to replace V2 models with V3 types throughout | Medium — replace `plugin_models.*` with go-cfclient types, update business logic |

### Companion Packages

Standalone replacement packages for the most common internal CLI imports:

| Package | Replaces | Purpose |
|---------|----------|---------|
| `cfclient` | V2 domain methods | Pre-configured go-cfclient V3 from plugin context |
| `cfconfig` | `cf/configuration/confighelpers` | `DefaultFilePath()`, `PluginRepoDir()` (~5 lines stdlib) |
| `cftrace` | `cf/trace` | HTTP request tracing without CLI dependency |
| `cfui` | `cf/terminal` (Pattern B) | `Say`, `Warn`, `Failed`, `Ok`, `Table`, color functions |

### Validation

- **Rabobank cf-plugins** — production guest-side V3 wrapper since 2025, validates the architectural approach
- **Proof-of-concept candidates** — OCF Scheduler (session-only, zero V2 methods), App Autoscaler (already migrated GetApp in PR #132)

### Impact

- **Plugin developers:** Run scanner to assess scope, choose migration path, execute at their own pace
- **CLI team:** No immediate changes required; can remove host-side V2 code when plugin migration reaches sufficient adoption
- **Foundation:** Reduced risk of ecosystem disruption when CAPI V2 is removed

---

# RFC C: CF CLI Plugin Interface V9 — Minimal Stable Contract

## Meta
[meta]: #meta
- Name: CF CLI Plugin Interface V9 — Minimal Stable Contract
- Start Date: 2026-03-25
- Author(s): @norman-abramovitz
- Status: Draft
- RFC Pull Request: (to be created)
- Tracking Issue: [cloudfoundry/cli#3621](https://github.com/cloudfoundry/cli/issues/3621)

## Summary

This RFC proposes a modernized CF CLI plugin interface for CLI v9 that provides plugins with only authentication, session context, and API endpoint information. All CF domain models (apps, routes, services) are removed from the plugin API surface. Plugins MUST interact with Cloud Foundry through CAPI V3 directly, using libraries such as go-cfclient. The interface introduces a channel abstraction enabling both the existing gob/net-rpc transport (backward compatibility) and JSON-RPC 2.0 (polyglot plugin support), and replaces execution-time metadata discovery with an embedded marker for install-time introspection.

## Problem

### Tight Coupling to CLI Internals and V2 Types

The current plugin interface embeds CF domain semantics into the plugin contract through 10 V2 domain methods. These methods proxy CAPI V2 endpoints and return V2-shaped data structures that cannot represent V3 concepts. This coupling means the plugin interface must change whenever CAPI changes — the opposite of a stable contract.

### Go-Only, gob/net-rpc Transport

The current interface uses Go's `net/rpc` with `encoding/gob` serialization. This limits plugins to Go and makes the wire protocol opaque to non-Go tooling. The `encoding/gob` format is Go-specific and has no implementations in other languages.

### Execution-Time Metadata Discovery

The CLI currently discovers plugin metadata by executing the plugin binary with no arguments and reading the gob-encoded `GetMetadata()` response over RPC. This requires executing untrusted code at install time — a security concern and a barrier to non-Go plugins.

### Insufficient Versioning and Help Metadata

The `VersionType` struct provides only `Major`, `Minor`, and `Build` fields with no support for SemVer prerelease or build metadata. The `Build` field name is misleading (it maps to SemVer's "patch"). Help metadata lacks `Warning`, `Tip`, `Examples`, and `RelatedCmds` fields that the CF CLI Style Guide specifies for built-in commands. Flag metadata is `map[string]string` — unordered, with no way to declare long/short pairs, defaults, required status, or grouping.

## Proposal

### Design Principles

1. **Host as context provider, not domain proxy.** The host MUST provide authentication, endpoint, and target context. It MUST NOT provide CF domain models or proxy CAPI endpoints.
2. **Minimal stable contract.** The core contract MUST contain only serializable primitives (strings, booleans, simple structs) that can cross a process boundary over any wire protocol.
3. **Protocol-agnostic communication.** Host-to-guest communication MUST be abstracted behind a channel interface. The host MUST NOT depend on any specific wire protocol.
4. **Language portability.** The plugin interface MUST NOT require guests to be written in Go.

### PluginContext Interface

The V9 plugin interface provides session, endpoint, and target context only:

**Session and Authentication:**
- `AccessToken() (string, error)`
- `RefreshToken() (string, error)`
- `IsLoggedIn() (bool, error)`
- `Username() (string, error)`

**API Endpoint and Configuration:**
- `ApiEndpoint() (string, error)`
- `HasAPIEndpoint() (bool, error)`
- `IsSSLDisabled() (bool, error)`
- `ApiVersion() (string, error)`

**Target Context:**
- `GetCurrentOrg() (OrgContext, error)` — returns `{GUID, Name}` only
- `GetCurrentSpace() (SpaceContext, error)` — returns `{GUID, Name}` only
- `HasOrganization() (bool, error)`
- `HasSpace() (bool, error)`

### Methods Removed

The following MUST NOT be carried forward into V9:

- **V2 domain methods:** `GetApp`, `GetApps`, `GetService`, `GetServices`, `GetOrg`, `GetOrgs`, `GetSpaces`, `GetOrgUsers`, `GetSpaceUsers`, `GetRoutes`
- **CLI command execution:** `CliCommand`, `CliCommandWithoutTerminalOutput`

Plugins MUST use CAPI V3 directly for all domain operations.

### Channel Abstraction

Communication between host and guest is abstracted behind a channel interface:

```
Channel {
    Open() error
    Close() error
    Send(method string, args interface{}, reply interface{}) error
    Receive() (method string, args interface{}, error)
}
```

Two implementations:

| Channel | Transport | Use Case |
|---------|-----------|----------|
| `GobTCPChannel` | gob/net-rpc over TCP | Backward compatibility with existing Go plugins |
| `JsonRpcChannel` | JSON-RPC 2.0 over stdio | Polyglot plugins (Python, Rust, Java, etc.) |

The host detects which channel to use based on plugin metadata (see below).

### Embedded Metadata Marker

Plugin metadata MUST be discoverable without executing the plugin binary. Plugins MUST embed a `CF_PLUGIN_METADATA:` marker in the binary containing JSON-encoded metadata. The CLI reads metadata by scanning the binary at install time — no execution required.

```
CF_PLUGIN_METADATA:{"name":"my-plugin","version":"1.2.3","commands":[...]}
```

### Improved Versioning

`VersionType` is replaced by `PluginVersion`:

- `Major`, `Minor`, `Patch` (renamed from `Build`)
- `PreRelease` — e.g., `rc.1`, `beta.2`
- `BuildMeta` — e.g., `linux.amd64`, `20260301`
- `String()` — returns full SemVer 2.0 representation

### Improved Help Metadata

`Command` struct gains:

- `Warning` — critical alerts about command behavior
- `Tip` — helpful context or deprecation notices
- `Examples` — usage examples
- `RelatedCmds` — "see also" commands

`Usage` struct gains `Flags []FlagDefinition` alongside legacy `Options map[string]string`:

```
FlagDefinition {
    Long, Short, Description, Default string
    HasArg, Required bool
    Group string
}
```

When `Flags` is populated, the host MUST use it for help display instead of `Options`.

### CF Client Access

`CfClient()` MUST NOT be part of the core contract — a pre-configured client object cannot be serialized over a wire protocol. Instead, a companion Go package SHOULD be provided that constructs a go-cfclient V3 client from the core contract primitives (`AccessToken`, `ApiEndpoint`, `IsSSLDisabled`).

### Migration Phases

| Phase | Timeframe | Milestone |
|-------|-----------|-----------|
| **Draft** | Q2 2026 | RFC submitted, community review |
| **Prototype** | Q3 2026 | Reference implementation in CLI branch, Go + one non-Go plugin |
| **Beta** | Q1 2027 | CLI v9 beta with V9 interface alongside V8 compatibility |
| **Stable** | Q3 2027 | CLI v9 stable, V2 domain methods removed from host |

### Backward Compatibility

The channel abstraction enables a smooth transition. CLI v9 SHOULD support both `GobTCPChannel` (existing plugins) and `JsonRpcChannel` (new plugins) simultaneously. Plugins using the transitional migration (RFC B) will work unchanged over `GobTCPChannel`. New plugins MAY use either channel.

---

# RFC D: CF CLI Plugin Repository Compatibility and Maintenance

## Meta
[meta]: #meta
- Name: CF CLI Plugin Repository Compatibility and Maintenance
- Start Date: 2026-03-25
- Author(s): @norman-abramovitz
- Status: Draft
- RFC Pull Request: (to be created)
- Tracking Issue: [cloudfoundry/cli#3621](https://github.com/cloudfoundry/cli/issues/3621)

## Summary

The CF CLI plugin repository lists plugins with no structured information about CAPI version compatibility, CLI version requirements, or maintenance status. As CAPI V2 reaches end of life, users have no way to determine which plugins will continue to work on V2-disabled foundations. This RFC proposes structured compatibility metadata for the plugin repository and a policy for handling unmaintained plugins.

## Problem

### No Compatibility Visibility

The plugin repository (`cli-plugin-repo`) lists plugins with a name, description, version, and binary URLs. There is no metadata indicating:

- Whether a plugin depends on CAPI V2 endpoints (directly or via the plugin interface's V2 domain methods)
- Which CF CLI versions the plugin is compatible with
- Which plugin interface version the plugin targets

When a foundation disables CAPI V2, users discover incompatibilities only at runtime — after installing and attempting to use a plugin that silently fails or errors.

### Unmaintained Plugins Remain Listed

The repository includes plugins that have not been updated in years. For example, `cf-top` was last updated in 2017. These plugins:

- May not compile against current Go versions
- May depend on CAPI V2 endpoints that no longer exist
- May have unpatched security vulnerabilities
- Create a false impression of ecosystem health

There is no policy for identifying, flagging, or removing unmaintained plugins.

## Proposal

### Compatibility Metadata Format

Each plugin entry in the repository SHOULD include a `compatibility` section:

```yaml
- name: my-plugin
  description: Does useful things
  version: 1.2.3
  compatibility:
    cli_plugin_interface: "v8"        # or "v9" once available
    capi:
      min: "3.0.0"                    # minimum CAPI version
      v2_required: false              # whether plugin calls V2 endpoints
    cf_cli:
      min: "8.0.0"                    # minimum CLI version
      max: ""                         # optional maximum (empty = no upper bound)
```

The `v2_required` field is the critical signal: it tells users and operators whether a plugin will work on a V2-disabled foundation.

### Automated V2 Dependency Scanning

The `cf-plugin-migrate scan` tool (RFC B) can objectively identify V2 dependencies across the entire plugin repository by scanning each plugin's source code. The scan detects:

- V2 domain method calls (`GetApp`, `GetApps`, etc.)
- `CliCommand` calls to `cf curl /v2/...` endpoints
- Import of `plugin/models` V2 types

Scan results SHOULD be published alongside the compatibility metadata to provide an objective, reproducible assessment independent of plugin author self-reporting.

### Plugin Author Self-Reporting

Plugin authors MAY self-report compatibility metadata by including a `plugin-compat.yml` file in their repository or by updating their plugin repository entry. Self-reported metadata SHOULD be validated against automated scan results where possible.

### Unmaintained Plugin Policy

Plugins SHOULD be considered unmaintained when:

- No release or commit activity for 24 months, AND
- No response to compatibility assessment notification within 90 days

The policy defines a graduated response:

| Phase | Timeframe | Action |
|-------|-----------|--------|
| **Notification** | Day 0 | Issue filed on plugin repository requesting compatibility update from author |
| **Warning** | Day 90 | Plugin listing annotated with "unmaintained — compatibility unverified" |
| **Archival** | Day 180 | Plugin moved to an archived section, no longer shown in default `cf repo-plugins` output |
| **Removal** | Day 365 | Plugin entry removed from repository (binary URLs may remain for direct install) |

Plugin authors MAY restore their listing at any point by updating the plugin and providing compatibility metadata.

### Integration with Plugin Install

The CF CLI `cf install-plugin` command SHOULD warn users when installing a plugin that:

- Has `v2_required: true` and the target foundation has V2 disabled
- Is annotated as unmaintained
- Has no compatibility metadata (unknown compatibility)

These warnings SHOULD be informational, not blocking — users MAY proceed with installation.

---

## Discussion Points for CLI WG (2026-03-25)

1. **Split vs. monolithic:** Does the WG agree that four focused RFCs are preferable to one large RFC?
2. **RFC B maturity:** PR #1452 has received review. Is it ready for FCP, or should it wait for RFC A to be submitted first?
3. **V9 timeline:** Is the Q3 2027 target for V9 stable realistic given CLI team capacity?
4. **Repository policy:** Who owns the unmaintained plugin policy — CLI WG or the Foundation?
5. **Scanning scope:** Should automated V2 scanning be run against all 20 repository plugins, or only actively maintained ones?
