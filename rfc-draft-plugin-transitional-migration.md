# Meta
[meta]: #meta
- Name: CLI Plugin Transitional Migration to CAPI V3
- Start Date: 2026-03-01
- Author(s): @norman-abramovitz
- Status: Draft
- RFC Pull Request: [cloudfoundry/community#XXX](https://github.com/cloudfoundry/community/pull/XXX)
- Tracking Issue: [cloudfoundry/cli#3621](https://github.com/cloudfoundry/cli/issues/3621)

## Summary

The CF CLI plugin interface exposes 10 methods that return CAPI V2-shaped data (`GetApp`, `GetApps`, `GetService`, `GetServices`, `GetOrg`, `GetOrgs`, `GetSpaces`, `GetOrgUsers`, `GetSpaceUsers`). With CAPI V2 reaching end of life ([RFC-0032](https://github.com/cloudfoundry/community/blob/main/toc/rfc/rfc-0032-cfapiv2-eol.md)), these methods will stop working — affecting at least 18 actively maintained plugins.

This RFC proposes a **guest-side transitional migration** that plugin developers can adopt today, without waiting for CLI team changes or a new plugin interface. A companion tool (`cf-plugin-migrate`) scans a plugin's source code, identifies V2 usage, and generates drop-in replacement code backed by CAPI V3 via [go-cfclient](https://github.com/cloudfoundry/go-cfclient). The approach requires **no host (CLI) changes** and works with any existing CF CLI version.

## Problem

### V2 Domain Methods Will Break

The plugin API's domain methods (`GetApp`, `GetApps`, `GetService`, etc.) return `plugin_models.*` types that mirror V2 response structures. When V2 endpoints are removed per [RFC-0032](https://github.com/cloudfoundry/community/blob/main/toc/rfc/rfc-0032-cfapiv2-eol.md), these methods will fail. The current interface provides no V3 alternative.

### The Host Carries Legacy Code for Plugin Support

The CLI implements 10 V2 domain methods on behalf of plugins via RPC handlers in `plugin/rpc/cli_rpc_server.go`, V2-shaped types in `plugin/models/`, and associated state management. This code exists solely to serve plugins. Once plugins fetch their own data via CAPI V3, the CLI team can remove this subsystem.

### Plugins Have Already Converged on the Minimal Pattern

A [survey of 18 actively maintained plugins](https://github.com/norman-abramovitz/cf-plugin-rfc-proposal/blob/main/plugin-survey.md) shows that the community has organically converged on using the CLI only for authentication and target context. The universally used methods are: `AccessToken()`, `ApiEndpoint()`, `IsSSLDisabled()`, `GetCurrentOrg()`, `GetCurrentSpace()`, and `Username()`. Domain methods like `GetApp()` are being actively removed (e.g., [App Autoscaler PR #132](https://github.com/cloudfoundry/app-autoscaler-cli-plugin/pull/132)).

The [MultiApps (MTA)](https://github.com/cloudfoundry/cli/issues/3621#issuecomment-3811806007) and [App Autoscaler](https://github.com/cloudfoundry/cli/issues/3621#issuecomment-3811939246) plugin teams have independently documented this same pattern and recommended it as the standard approach.

### Plugins Also Import CLI Internal Packages

Beyond the intended plugin API, 8 of 18 surveyed plugins import internal CLI packages (`cf/terminal`, `cf/trace`, `cf/configuration/confighelpers`). These imports will break if the CLI team refactors those packages. Replacement packages with matching signatures can eliminate this coupling for most cases.

## Proposal

### Guest-Side V2 Compatibility Wrapper

The migration introduces a thin wrapper on the guest (plugin) side that:

- **Embeds** `plugin.CliConnection` — all context methods (`AccessToken`, `GetCurrentOrg`, etc.) pass through unchanged
- **Constructs** a go-cfclient V3 client from `AccessToken()`, `ApiEndpoint()`, and `IsSSLDisabled()`
- **Reimplements** only the V2 domain methods the plugin actually uses, backed by the minimum V3 API calls required for the fields the plugin accesses
- **Satisfies** `plugin.CliConnection` — existing code works without changes

The CLI host requires no changes. Each plugin migrates independently.

### Migration Tool: `cf-plugin-migrate`

**Scan** — AST-based analysis identifies V2 domain method calls, `CliCommand`/`cf curl` usage, internal CLI package imports, and traces which fields of returned models are actually accessed.

**Generate** — Reads the scan output (YAML) and produces a single `v2compat_generated.go` file with minimal V3 replacements. If a plugin uses `GetApp` only for `.Guid` and `.Name`, the generated code makes 1 V3 API call and populates 2 fields — compared to the 10+ calls a full reimplementation requires.

### Migration Paths

**Path A: Generated wrapper (recommended).** Run `cf-plugin-migrate scan`, review the YAML, run `cf-plugin-migrate generate`, add the generated file, and wire the wrapper with one line:

```go
func (p *MyPlugin) Run(conn plugin.CliConnection, args []string) {
    conn, err := NewV2Compat(conn) // shadow the parameter
    // All existing code works unchanged — conn.GetApp() now uses V3
}
```

**Path B: Direct V3 access.** For new plugins or plugins wanting V3-native types, construct a go-cfclient client from the connection's credentials and use CAPI V3 directly:

```go
cfg, _ := config.New(endpoint, config.Token(token), config.SkipTLSValidation(skipSSL))
cfClient, _ := client.New(cfg)
apps, _ := cfClient.Applications.ListAll(ctx, &client.AppListOptions{SpaceGUIDs: ...})
```

### Companion Packages

A `cf-plugin-helpers` module SHOULD provide:

- **`cfclient`** — constructs a go-cfclient V3 client from `plugin.CliConnection` credentials
- **`cfconfig`** — replaces `cf/configuration/confighelpers` (5 plugins affected, ~30 lines stdlib)
- **`cftrace`** — replaces `cf/trace` with V3 call tracing support (6 plugins affected)
- **`cfui`** — replaces `cf/terminal` for table/color output (5 plugins affected)

Import aliases preserve existing code references: `trace "code.cloudfoundry.org/cf-plugin-helpers/cftrace"` requires zero code changes beyond the import path.

### Validated by Production Use

The [Rabobank cf-plugins](https://github.com/rabobank/cf-plugins) library has been in production since 2025, validating the guest-side wrapper pattern. The generated approach improves on it by producing only the code each plugin needs — e.g., `GetService` in 1 API call (using V3 `fields` parameters) versus Rabobank's 3 calls.

### Proof-of-Concept Candidates

| Tier | Plugin | V2 Methods | Complexity |
|------|--------|------------|------------|
| 1 | [list-services](https://github.com/pavellom/list-services-plugin) | `GetApp` (Guid only) | Simplest — already 90% V3 |
| 2 | [ocf-scheduler](https://github.com/cloudfoundry-community/ocf-scheduler-cf-plugin) | `GetApp`, `GetApps` | Moderate — core app fields only |
| 3 | [metric-registrar](https://github.com/pivotal-cf/metric-registrar-cli) | `GetApp`, `GetApps`, `GetServices` + V2 curl | Complex — domain model redesign for ports |

## Impact and Consequences

### Positive

- Plugins eliminate their V2 dependency before the [RFC-0032](https://github.com/cloudfoundry/community/blob/main/toc/rfc/rfc-0032-cfapiv2-eol.md) removal deadline.
- No cross-team coordination required — plugin teams migrate at their own pace.
- The CLI team is unblocked to remove legacy V2 plugin support code on its own timeline.
- Session-only plugins (those using only context methods) require zero code changes.

### Negative

- Plugins gain a dependency on go-cfclient/v3 (currently alpha, but in production use across multiple plugins).
- V3 API calls happen guest-side, so `CF_TRACE` does not capture them by default. The `cftrace` companion package restores this debugging capability.
- Some V2 concepts have no direct V3 equivalent (e.g., app `ports` → route destinations). These require manual redesign rather than generated wrappers.

### Relationship to a New Plugin Interface

This RFC addresses the immediate V2 end-of-life risk using the existing plugin interface. A separate future RFC will propose a modernized plugin interface with a minimal stable contract, polyglot language support, and improved help and versioning metadata.

## References

- [RFC-0032 — CF API V2 End of Life](https://github.com/cloudfoundry/community/blob/main/toc/rfc/rfc-0032-cfapiv2-eol.md)
- [cloudfoundry/cli#3621 — Plugin Interface Discussion](https://github.com/cloudfoundry/cli/issues/3621)
- [go-cfclient — Cloud Foundry V3 Go client](https://github.com/cloudfoundry/go-cfclient)
- [Rabobank cf-plugins — Production V2-to-V3 wrapper](https://github.com/rabobank/cf-plugins)
- [App Autoscaler PR #132 — V3 migration](https://github.com/cloudfoundry/app-autoscaler-cli-plugin/pull/132)
- [Plugin survey — 18 active plugins analyzed](https://github.com/norman-abramovitz/cf-plugin-rfc-proposal/blob/main/plugin-survey.md)
- [Detailed design document](rfc-draft-plugin-transitional-migration-detailed.md) — Full technical specification with code examples, field mappings, and worked migration examples
