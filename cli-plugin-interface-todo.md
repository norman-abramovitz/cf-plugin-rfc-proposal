# TODO: Clean and Maintainable Plugin Interface from the CF CLI

This document tracks what needs to be done inside the
[CF CLI](https://github.com/cloudfoundry/cli) codebase to create a clean,
standalone, and maintainable plugin interface.

## Current State

The plugin interface lives in `plugin/` and `plugin/models/` within the CLI repo.
A key finding from the code analysis: **the plugin-side code is already cleanly
separated from CLI internals** — the `plugin/`, `plugin/models/` packages have
zero imports from `cf/`, `command/`, `actor/`, or any external dependency. The
only dependencies are Go standard library (`net/rpc`, `time`, `fmt`, `os`).

The coupling exists entirely on the **CLI's RPC server side** (`plugin/rpc/`),
which depends heavily on `cf/api`, `cf/commandregistry`, `cf/configuration/coreconfig`,
and `cf/terminal`.

### Files that form the plugin-side surface (~600 lines, stdlib only)

| File | Contents |
|---|---|
| `plugin/plugin.go` | `Plugin`, `CliConnection`, `PluginMetadata`, `VersionType` (Major/Minor/Build ints only — no SemVer prerelease or build metadata), `Command`, `Usage` (Options is `map[string]string` — unordered, no long/short pairing, no defaults) |
| `plugin/cli_connection.go` | `cliConnection` struct — RPC client that dials `127.0.0.1:<port>` |
| `plugin/plugin_shim.go` | `Start()`, `MinCliVersionStr()`, metadata exchange |
| `plugin/models/*.go` (13 files) | All model types (`GetAppModel`, `Organization`, `Space`, etc.) — only import is `time` |

### Files that stay in the CLI (deeply coupled to internals)

| File | Depends On |
|---|---|
| `plugin/rpc/cli_rpc_server.go` | `cf/api.RepositoryLocator`, `cf/commandregistry`, `cf/configuration/coreconfig`, `cf/terminal` |
| `plugin/rpc/run_plugin.go` | `cf/configuration/pluginconfig` |
| `plugin/rpc/call_command_registry.go` | `cf/commandregistry`, `cf/flags`, `cf/requirements` |

### How CliConnection methods are implemented on the server side

**Config-derived (simple reads from `coreconfig.Repository`):**
- `GetCurrentOrg()` → `cliConfig.OrganizationFields()`
- `GetCurrentSpace()` → `cliConfig.SpaceFields()`
- `Username()` → `cliConfig.Username()`
- `UserGuid()` → `cliConfig.UserGUID()`
- `UserEmail()` → `cliConfig.UserEmail()`
- `IsLoggedIn()` → `cliConfig.IsLoggedIn()`
- `IsSSLDisabled()` → `cliConfig.IsSSLDisabled()`
- `HasOrganization()` → `cliConfig.HasOrganization()`
- `HasSpace()` → `cliConfig.HasSpace()`
- `ApiEndpoint()` → `cliConfig.APIEndpoint()`
- `ApiVersion()` → `cliConfig.APIVersion()`
- `HasAPIEndpoint()` → `cliConfig.HasAPIEndpoint()`
- `DopplerEndpoint()` → `cliConfig.DopplerEndpoint()`
- `LoggregatorEndpoint()` → returns `""` (deprecated)

**Auth-derived (calls authentication repository):**
- `AccessToken()` → `repoLocator.GetAuthenticationRepository().RefreshAuthToken()`

**Command-derived (runs full CLI commands with PluginModels injection):**
- `GetApp(name)` → runs `app <name>` command, injects `deps.PluginModels.Application`
- `GetApps()` → runs `apps` command, injects `deps.PluginModels.AppsSummary`
- `GetOrgs()` → runs `orgs` command, injects `deps.PluginModels.Organizations`
- `GetSpaces()` → runs `spaces` command, injects `deps.PluginModels.Spaces`
- `GetOrg(name)` → runs `org <name>` command
- `GetSpace(name)` → runs `space <name>` command
- `GetService(name)` → runs `service <name>` command
- `GetServices()` → runs `services` command
- `GetOrgUsers()` → runs `org-users` command
- `GetSpaceUsers()` → runs `space-users` command
- `CliCommand(args)` / `CliCommandWithoutTerminalOutput(args)` → runs arbitrary CLI commands

---

## Phase 1: Extract Standalone Plugin Interface Module

### 1.1 Create new Go module

- [ ] Create a new repository (e.g., `code.cloudfoundry.org/cli-plugin-interface` or under a `v2` path)
- [ ] The module MUST have zero external dependencies — only Go standard library
- [ ] Copy the clean plugin-side files:
  - `plugin/plugin.go` → interface definitions (`Plugin`, `CliConnection`, metadata types)
  - `plugin/cli_connection.go` → RPC client implementation
  - `plugin/plugin_shim.go` → `Start()` entrypoint and metadata exchange
  - `plugin/models/*.go` → all 13 model files
- [ ] Update package paths and imports
- [ ] Generate new test fakes (`FakeCliConnection`) using counterfeiter
- [ ] Publish the module so plugins can import it independently of the CLI

### 1.2 Update CLI to import the new module

- [ ] Replace `plugin/` and `plugin/models/` with an import of the new module
- [ ] Update `plugin/rpc/cli_rpc_server.go` to use the new module's types
- [ ] Update `plugin/rpc/run_plugin.go` and `call_command_registry.go` references
- [ ] Update `cf/commandregistry/dependency.go` `PluginModels` struct to use new types
- [ ] Update all command implementations that populate `PluginModels` pointers
- [ ] Verify RPC wire format remains compatible (Go's `net/rpc` uses `encoding/gob` — types must be registered under the same names)

### 1.3 Verify backward compatibility

- [ ] Existing compiled plugins MUST continue to work with the updated CLI (same RPC method names, same type shapes)
- [ ] New plugins built against the standalone module MUST work with both old and new CLI versions
- [ ] Write integration tests that verify a plugin built with the new module can run against the CLI

---

## Phase 2: Define the New Minimal Interface (V2)

### 2.1 New `PluginContext` interface (replaces `CliConnection`)

- [ ] Define the minimal `PluginContext` interface with only the universally-needed methods:

```go
type PluginContext interface {
    // Session and Authentication
    AccessToken() (string, error)
    RefreshToken() (string, error)
    IsLoggedIn() (bool, error)
    Username() (string, error)

    // API Endpoint and Configuration
    ApiEndpoint() (string, error)
    HasAPIEndpoint() (bool, error)
    IsSSLDisabled() (bool, error)
    ApiVersion() (string, error)

    // Target Context
    GetCurrentOrg() (OrgContext, error)
    GetCurrentSpace() (SpaceContext, error)
    HasOrganization() (bool, error)
    HasSpace() (bool, error)

}
```

Note: `CfClient()` is NOT part of the core interface — it lives in a companion package
(`cli-plugin-helpers/cfclient`). The core contract contains only serializable primitives
that can cross the process boundary over any wire protocol (gob, JSON-RPC, etc.).

- [ ] Define minimal context types (replacing the V2-shaped models):

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

- [x] ~~Decide: Does `CfClient()` live in the core interface or a separate helper package?~~ **Decision: Companion package.** The core contract provides only serializable primitives (`AccessToken()`, `ApiEndpoint()`, `IsSSLDisabled()`). A `NewCfClient(ctx)` helper in `cli-plugin-helpers/cfclient` constructs a go-cfclient V3 client from those primitives. This keeps the core interface dependency-free and language-agnostic.

### 2.2 New `Plugin` interface

- [ ] Update the `Plugin` interface to accept `PluginContext`:

```go
type Plugin interface {
    Run(ctx PluginContext, args []string)
    GetMetadata() PluginMetadata
}
```

- [ ] Update `PluginMetadata` with full semver support:

The current `VersionType` struct uses only `Major`, `Minor`, and `Build` integer fields. The `Build` field name is a misnomer — it corresponds to SemVer's "patch" number, not build metadata. Plugins that track prerelease or platform-specific build identifiers (e.g., `1.0.0-rc.1+linux.amd64`) cannot communicate this through the plugin API. The ocf-scheduler and cf-targets plugins work around this by printing the full version string when invoked directly without arguments — but this information is invisible to `cf plugins`.

```go
type PluginVersion struct {
    Major      int
    Minor      int
    Patch      int        // Renamed from Build for SemVer correctness
    PreRelease string     // e.g., "rc.1", "beta.2"
    BuildMeta  string     // e.g., "linux.amd64", "20260301"
}

// String returns the full SemVer 2.0 string representation.
// Examples: "1.2.3", "1.2.3-rc.1", "1.2.3+linux.amd64"
func (v PluginVersion) String() string
```

- [ ] Update `PluginVersion.String()` in `util/configv3/plugins_config.go` — currently renders `Major.Minor.Build` or `"N/A"` — to include `PreRelease` and `BuildMeta`
- [ ] Update `cf plugins` output to show full SemVer strings
- [ ] Ensure backward compatibility: existing plugins with only `Major`/`Minor`/`Build` still work (missing fields default to empty strings)

### 2.3 Deprecate V2-coupled methods

- [ ] Keep old `CliConnection` interface as a type alias or embed of `PluginContext` plus deprecated methods
- [ ] Mark these methods as deprecated with clear migration guidance:
  - `GetApp()` / `GetApps()` → use `CfClient().Applications.ListAll()`
  - `GetService()` / `GetServices()` → use `CfClient().ServiceInstances.ListAll()`
  - `GetOrg()` / `GetOrgs()` → use `CfClient().Organizations.ListAll()`
  - `GetSpace()` / `GetSpaces()` → use `CfClient().Spaces.ListAll()`
  - `GetOrgUsers()` / `GetSpaceUsers()` → use `CfClient().Roles.ListIncludeUsersAll()`
  - `CliCommand()` / `CliCommandWithoutTerminalOutput()` → use `CfClient()` or direct HTTP
- [ ] Define a timeline for removal (see Phase 4)

### 2.4 Additional endpoint methods

- [ ] Decide which additional endpoints to expose:
  - `DopplerEndpoint()` — already exists but `LoggregatorEndpoint()` returns `""`
  - `UaaEndpoint()` — new, plugins currently discover this by parsing `/v3/info`
  - `RoutingApiEndpoint()` — new
  - `CredHubEndpoint()` — new, requested in issue discussion

---

## Phase 3: Update CLI RPC Server Implementation

### 3.1 Implement new PluginContext methods on the server side

The config-derived methods already exist and are trivial reads from `coreconfig.Repository`. The following need new implementation:

- [ ] `RefreshToken()` — read from `cliConfig.RefreshToken()` (likely already available)
- [x] ~~`CfClient()`~~ — **Resolved: companion package (Option C).** `CfClient()` is NOT an RPC method. A `NewCfClient(ctx PluginContext)` function in the `cli-plugin-helpers/cfclient` companion package constructs a go-cfclient V3 client using `AccessToken()` + `ApiEndpoint()` + `IsSSLDisabled()` from the core contract. No RPC change needed.
- [ ] `UaaEndpoint()` — read from `cliConfig.UAAEndpoint()` or derive from `/v3/info`
- [ ] `RoutingApiEndpoint()` — read from `cliConfig.RoutingAPIEndpoint()`

### 3.2 Remove command-derived method implementations

The "command-derived" methods (`GetApp`, `GetApps`, `GetOrgs`, etc.) work by running
full CLI commands internally and injecting `PluginModels` pointers into the command
dependency tree. This mechanism is:

- Tightly coupled to `cf/commandregistry`
- Fragile (depends on each command checking `deps.PluginModels != nil`)
- V2-shaped (returns V2 models even though the commands may now use V3 internally)

- [ ] Keep these methods functional during the deprecation period but stop adding new ones
- [ ] Document that these methods may return incomplete data for V3-only foundations
- [ ] Remove the `PluginModels` injection mechanism from `cf/commandregistry/dependency.go` once deprecated methods are removed

### 3.3 Remove `CliCommand` / `CliCommandWithoutTerminalOutput` server-side support

These methods work by running arbitrary CLI commands and capturing terminal output. They are:

- The source of reliability bugs (cf-java-plugin `cf ssh` authentication failures)
- Fragile across CLI version changes (output format changes break plugins)
- A security concern (arbitrary command execution)

- [ ] Keep functional during deprecation period
- [ ] Add CLI-side warnings when these methods are used
- [ ] Remove when deprecated methods are removed

---

## Phase 3b: Communication Architecture

### 3b.1 Channel abstraction

- [ ] Define the `PluginChannel` interface (`Open`/`Send`/`Receive`/`Close`)
- [ ] Implement `GobTCPChannel` wrapping the existing `net/rpc` transport — this MUST produce no behavior change for existing plugins
- [ ] Implement `JsonRpcChannel` using JSON-RPC 2.0 over TCP
- [ ] Refactor `plugin/rpc/run_plugin.go` and `plugin/rpc/cli_rpc_server.go` to use the channel abstraction instead of directly calling `net/rpc`

### 3b.2 Embedded metadata support

- [ ] Add `CF_PLUGIN_METADATA:` marker scanning to `cf install-plugin` (`command/common/install_plugin_command.go`)
- [ ] Define the embedded metadata JSON schema (with `schema_version` field)
- [ ] When marker is found: parse JSON, store metadata + `protocol` field in `~/.cf/plugins/config.json`, skip the existing gob/RPC metadata exchange
- [ ] When marker is not found: fall back to existing `exec.Command(path, PORT, "SendMetadata")` flow
- [ ] Update `configv3.Plugin` struct to include `Protocol` field

### 3b.3 Runtime protocol selection

- [ ] Read `Protocol` field from stored plugin config at runtime
- [ ] Select channel implementation (`GobTCPChannel` vs `JsonRpcChannel`) based on stored protocol
- [ ] Pass connection info to new-protocol plugins via environment variables (`CF_PLUGIN_PORT`, `CF_PLUGIN_PROTOCOL`) instead of positional arguments
- [ ] Legacy plugins continue to receive port as `os.Args[1]`

### 3b.4 JSON-RPC contract definition

- [ ] Define JSON-RPC method names for all core contract methods (`AccessToken`, `ApiEndpoint`, `GetCurrentOrg`, etc.)
- [ ] Define JSON-RPC method names for plugin lifecycle (`Run`, `SetPluginMetadata`)
- [ ] Define standard error codes (`NOT_LOGGED_IN`, `TOKEN_EXPIRED`, `NO_TARGET`, `METHOD_NOT_AVAILABLE`)
- [ ] Document the full JSON-RPC contract so non-Go plugin authors can implement it

### 3b.5 Companion package

- [ ] Create `cli-plugin-helpers/cfclient` Go module with `NewCfClient(ctx PluginContext)` function
- [ ] The companion package MUST NOT be part of the core interface module
- [ ] The companion package tracks go-cfclient versions independently

---

## Phase 4: Plugin Help Integration

**Goal:** Enable plugins to produce help output consistent with the
[CF CLI Help Guidelines](https://github.com/cloudfoundry/cli/wiki/CF-CLI-Help-Guidelines),
[CF CLI Style Guide](https://github.com/cloudfoundry/cli/wiki/CF-CLI-Style-Guide), and
[CLI Product-Specific Style Guide](https://github.com/cloudfoundry/cli/wiki/CLI-Product-Specific-Style-Guide).
Currently, plugins can only produce a subset of the standard help sections and cannot
conform to the flag display conventions.

### Current help system behavior

**`cf help` (common commands view):**
- Plugin commands appear at the bottom under **"Commands offered by installed plugins:"**
- Displayed as a 3-column table of command names + aliases only — no `HelpText`
- Commands are sorted alphabetically, flat (not grouped by plugin)

**`cf help -a` (all commands view):**
- Plugin commands appear at the bottom under **"INSTALLED PLUGIN COMMANDS:"**
- Each command shows **Name** and **HelpText** in two columns
- Commands are flat-listed — no grouping by plugin name

**`cf help <command-name>` (single command help):**
- Works for plugin commands — matches by command `Name` or `Alias`
- Displays: **NAME** (name + HelpText), **USAGE** (UsageDetails.Usage), **ALIAS**, **OPTIONS** (from UsageDetails.Options map)
- Flag formatting: single-char keys → `-x`, multi-char → `--xxx`, sorted alphabetically

**`cf help <plugin-name>` — DOES NOT EXIST.**
- There is no way to view all commands from a specific plugin via the help system
- Users must use `cf plugins` to see the plugin-to-command mapping

**`cf plugins`:**
- Displays a table with columns: plugin name, version, command name (with alias), command help text
- Also supports `--checksum` (sha1) and `--outdated` modes

### Current help metadata fields

The plugin SDK's `Command` struct supports:

```go
type Command struct {
    Name         string
    Alias        string
    HelpText     string
    UsageDetails Usage
}
type Usage struct {
    Usage   string
    Options map[string]string   // flag name → description only
}
```

**How `Options map[string]string` is processed** (in `ConvertPluginToCommandInfo()`, `command/common/internal/help_display.go`):
1. Map keys are collected into a slice and sorted alphabetically
2. Each key is classified by length: 1 character → short flag (`-f`), otherwise → long flag (`--force`)
3. Each entry becomes a separate `CommandFlag` with only `Short` OR `Long` populated — never both
4. The paired rendering path `--force, -f` in `FlagWithHyphens()` is unreachable for plugin flags
5. The `Default` field on `CommandFlag` is always empty (no way to set it through the map)

**What plugins CANNOT provide** (built-in commands get these from Go struct tags):
- Examples
- Warning text (critical alerts about command behavior)
- Tip text (helpful context or deprecation notices)
- Related commands / "SEE ALSO"
- Environment variables
- Flag default values, argument types, required/optional flags
- Long/short flag pairing (e.g., `--force, -f` as a single flag)
- Flag grouping (e.g., "Output options", "Filtering")
- Long-form description beyond the single-line `HelpText`
- Category grouping within the plugin section

**CF CLI wiki guides that define the expected help format:**

The [CF CLI Help Guidelines](https://github.com/cloudfoundry/cli/wiki/CF-CLI-Help-Guidelines)
define a standardized help format with 8 sections: NAME, USAGE (following
[docopt](http://docopt.org/) conventions — `[]` optional, `()` required groups,
`|` mutually exclusive, `...` repeating), WARNING, EXAMPLE, TIP, ALIAS, OPTIONS
(alphabetical, long option first with aliases comma-separated, defaults appended as
`(Default: value)`), and SEE ALSO (comma-separated, alphabetical). Plugins can only
produce NAME, USAGE, ALIAS, and a limited OPTIONS.

The [CF CLI Style Guide](https://github.com/cloudfoundry/cli/wiki/CF-CLI-Style-Guide)
establishes conventions for command naming (VERB-NOUN), fail-fast validation order
(invalid flags → prerequisites → server-side resources), output formatting (tables for
lists, key/value for single objects, "OK"/"FAILED" feedback), color usage (cyan for
resource names, green for "OK", red for "FAILED"), and flag design (enum-style flags
with values over boolean flags). Plugins must implement all of this independently.

The [CLI Product-Specific Style Guide](https://github.com/cloudfoundry/cli/wiki/CLI-Product-Specific-Style-Guide)
adds product conventions: standard error messages ("Not logged in. Use 'cf login'..."),
TIP conventions (follow-up commands in single quotes), destructive operation confirmation
prompts (`--force` to bypass), idempotent operation exit codes (0 if already in desired
state), and table column ordering (new columns at end for versioning).

**Plugin workarounds observed:**
- ocf-scheduler: embeds all flag documentation directly in the `Usage` string, bypasses `Options` entirely
- cf-targets: uses only single-character keys in `Options` (e.g., `"f"`) to ensure they render as short flags

### Help system improvements

#### 4.1 Add `cf help <plugin-name>` support

- [ ] Modify `command/common/help_command.go` `findPlugin()` to also match against `PluginMetadata.Name`
- [ ] When matched by plugin name, display all commands from that plugin in a group:
  ```
  PLUGIN:
     MyPlugin v1.2.3

  COMMANDS:
     my-command, mc     Do something useful
     other-command      Do something else

  Use 'cf help <command>' for details on a specific command.
  ```
- [ ] This addresses the gap noted by plugin developers in the issue discussion

#### 4.2 Enrich `Command` struct for richer help

The [CF CLI Help Guidelines](https://github.com/cloudfoundry/cli/wiki/CF-CLI-Help-Guidelines) define 8 standard help sections. The enriched `Command` struct should enable plugins to produce all of them:

- [ ] Add optional fields to the `Command` struct:

```go
type Command struct {
    Name         string
    Alias        string
    HelpText     string          // Short description (existing) → NAME section
    Description  string          // Long-form description (new) → extended NAME
    Warning      string          // Critical alerts (new) → WARNING section
    Examples     string          // Usage examples (new) → EXAMPLE section
    Tip          string          // Helpful context (new) → TIP section
    RelatedCmds  []string        // See also (new) → SEE ALSO section
    UsageDetails Usage           // → USAGE and OPTIONS sections
}
```

- [ ] Update `ConvertPluginToCommandInfo()` in `command/common/internal/help_display.go` to populate `Examples`, `RelatedCommands`, `Warning`, and `Tip` fields
- [ ] `Usage` string SHOULD follow [docopt](http://docopt.org/) conventions (`[]` optional, `()` required groups, `|` mutually exclusive, `...` repeating) per the Help Guidelines
- [ ] These new fields are optional — existing plugins that don't set them continue to work

#### 4.3 Replace `Usage.Options` map with structured `Flags` slice

The current `Options map[string]string` has fundamental limitations:

1. **Unordered.** Go maps have no guaranteed iteration order. The CLI works around this by collecting keys into a slice and sorting alphabetically (`command/common/internal/help_display.go` `ConvertPluginToCommandInfo()`). Plugins cannot control display order.
2. **No long/short flag pairing.** Each map key becomes a separate flag entry. `ConvertPluginToCommandInfo()` classifies by key length: single-character keys → short flag (`-f`), everything else → long flag (`--force`). A single `FlagDefinition` with both `Long: "force"` and `Short: "f"` is impossible — the paired rendering path in `FlagWithHyphens()` (`--force, -f`) is unreachable for plugin flags.
3. **No defaults, required status, or value placeholders.** The map value is only the description string. Built-in commands display `(Default: json)` annotations through Go struct tags — plugins cannot.
4. **No grouping.** All flags render in a single flat list. Plugins with many flags (e.g., `create-job` with disk, memory, health check, and scheduling options) cannot organize them logically.
5. **Workaround in practice.** The ocf-scheduler plugin bypasses `Options` entirely, embedding all flag documentation directly in the `Usage` string to maintain control over ordering and formatting.

- [ ] Add `Flags []FlagDefinition` to the `Usage` struct alongside the existing `Options` for backward compatibility:

```go
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

- [ ] Update `ConvertPluginToCommandInfo()` in `command/common/internal/help_display.go`:
  - If `Flags` is populated, use it instead of `Options`
  - Each `FlagDefinition` with both `Long` and `Short` produces a single `CommandFlag` with both fields set, enabling the `--force, -f` paired rendering
  - Render `Default` and `Required` annotations matching the built-in command style
  - Render flags grouped by `Group` header when present
- [ ] If only `Options` is populated, preserve the current behavior (alphabetical sort, length-based classification)
- [ ] Update `configv3.PluginCommand` and `pluginconfig.PluginMetadata` to store `Flags` in `~/.cf/plugins/config.json`
- [ ] Backward compatible: existing plugins that use only `Options` continue to work without changes

#### 4.4 Group plugin commands by plugin in `cf help -a`

- [ ] In `displayAllCommands()`, instead of one flat "INSTALLED PLUGIN COMMANDS:" section, group by plugin:

```
INSTALLED PLUGIN COMMANDS:
  MyPlugin v1.2.3:
     my-command       Do something useful
     other-command    Do something else

  OtherPlugin v2.0.0:
     another-cmd      Another command
```

- [ ] This makes it clear which plugin provides which command, matching `cf plugins` behavior

#### 4.5 Update plugin config storage

- [ ] Ensure `~/.cf/plugins/config.json` stores any new metadata fields (`Description`, `Warning`, `Examples`, `Tip`, `RelatedCmds`)
- [ ] The `configv3.PluginCommand` struct must be updated in parallel:
  - `util/configv3/plugins_config.go` — `PluginCommand` struct
  - `cf/configuration/pluginconfig/plugin_data.go` — legacy `PluginMetadata`
- [ ] Backward compatible: missing fields in existing config files default to empty strings

---

## Phase 5: Dependency Cleanup

### 5.1 Eliminate transitive dependency bloat for plugins

Currently, plugins that `import "code.cloudfoundry.org/cli/v9/plugin"` pull in the
entire CLI module as a dependency in `go.mod`, even though the plugin package has
no internal imports. This is because Go modules resolve at the module level, not
the package level.

- [ ] The standalone module (Phase 1) solves this — plugins would import `code.cloudfoundry.org/cli-plugin-interface` which has zero external deps
- [ ] Verify that the new module's `go.sum` is minimal
- [ ] Provide migration guide for updating `go.mod` (replace old import with new)

### 5.2 Clean up archived/unmaintained transitive dependencies

The current plugin interface, when imported via the CLI module, pulls in:
- `k8s.io/apimachinery`, `k8s.io/client-go` (Kubernetes)
- `code.cloudfoundry.org/diego-ssh`
- `github.com/cloudfoundry/bosh-cli`
- `google.golang.org/grpc`, `google.golang.org/protobuf`
- `github.com/satori/go.uuid` (vulnerable, unmaintained)

- [ ] The standalone module eliminates all of these for plugins
- [ ] Document the before/after dependency footprint

---

## Phase 6: Migration and Deprecation Timeline

### 6.1 Phase timeline

| Phase | Target | Description |
|---|---|---|
| Extract standalone module | Q3 2026 | New module published, CLI imports it, plugins can migrate imports |
| New V2 interface available | Q3 2026 | `PluginContext` interface with `CfClient()` helper available |
| Dual support | Q4 2026 | CLI supports both legacy `CliConnection` and new `PluginContext` |
| Deprecation warnings | Q1 2027 | CLI emits warnings for deprecated methods (`GetApp`, `CliCommand`, etc.) |
| Removal | Q3 2027+ | Deprecated methods removed from the interface |

### 6.2 Interface evolution strategy

The [CF CLI Version Switching Guide](https://github.com/cloudfoundry/cli/wiki/Version-Switching-Guide)
describes the CLI's approach to major versions: separate binaries (`cf7`, `cf8`) with
symlink routing. The plugin interface MUST NOT require this pattern for its own evolution.

- [ ] Define backward-compatible struct evolution rules — new fields in `PluginMetadata`,
  `Command`, `Usage`, `PluginVersion`, and `FlagDefinition` MUST use zero-valued defaults
  so existing compiled plugins work without recompilation
- [ ] Define additive RPC method rules — new methods MAY be added; plugins calling
  methods unsupported by an older CLI SHOULD receive a clear error, not a crash
- [ ] Consider runtime capability discovery — plugins SHOULD be able to query what
  capabilities the host CLI provides (e.g., whether `RefreshToken()` is available)
- [ ] Define deprecation signaling — the CLI MUST emit runtime warnings (not errors)
  for deprecated methods, so plugin users know to request updates from maintainers
- [ ] Document how `MinCliVersion` enforcement should work — currently stored but not
  meaningfully enforced; the CLI SHOULD warn (not block) when exceeded

### 6.3 Migration documentation

- [ ] Write a migration guide with before/after code examples
- [ ] Migrate at least one reference plugin (e.g., ocf-scheduler-cf-plugin) as a worked example
- [ ] Document common migration patterns:
  - `GetApp(name)` → `cfclient.Applications.ListAll()` with name+space filter
  - `GetService(name)` → `cfclient.ServiceInstances.Single()` with name+space filter
  - `CliCommandWithoutTerminalOutput("curl", path)` → `cfclient` or `http.Client`
  - Deriving service URLs from `ApiEndpoint()`
- [ ] Document how plugins should follow [CF CLI Style Guide](https://github.com/cloudfoundry/cli/wiki/CF-CLI-Style-Guide) conventions (VERB-NOUN naming, fail-fast validation, output formatting, color usage)
- [ ] Document how plugins should follow [Product-Specific Style Guide](https://github.com/cloudfoundry/cli/wiki/CLI-Product-Specific-Style-Guide) patterns (error messages, TIPs, confirmation prompts, idempotent operations)

### 6.4 Community communication

- [ ] Post RFC to `cloudfoundry/community` as a PR
- [ ] Present at CF community call
- [ ] Notify known plugin maintainers:
  - SAP (@s-yonkov-yonkov, @silvestre) — MTA, DefaultEnv, html5, java
  - Pivotal/Broadcom — metric-registrar, service-instance-logs, spring-cloud-services, mysql-cli
  - Swisscom (@jcvrabo) — appcloud
  - Community — OCF Scheduler, cf-targets, Rabobank, cf-lookup-route

---

## Resolved Questions

1. ~~**`CfClient()` placement**~~ → **Companion package** (`cli-plugin-helpers/cfclient`). Core contract contains only serializable primitives.

2. ~~**RPC protocol versioning**~~ → **Channel abstraction with embedded metadata.** The CLI uses a `PluginChannel` interface (`Send`/`Receive`/`Open`/`Close`) with concrete implementations for `GobTCPChannel` (legacy) and `JsonRpcChannel` (new). Protocol is determined at install time from the `CF_PLUGIN_METADATA:` marker embedded in the plugin binary/script.

3. ~~**`CliCommand` replacement**~~ → Not carried forward in the new JSON-RPC contract. Plugins use their own clients (go-cfclient, HTTP, gRPC, etc.) for all domain operations. Legacy plugins keep `CliCommand` via the `GobTCPChannel`.

## Open Questions

1. **Module naming:** `code.cloudfoundry.org/cli-plugin-interface` vs `code.cloudfoundry.org/cli-plugin-api` vs `code.cloudfoundry.org/cli/v9/plugin` (stay in-tree)?

2. **Additional endpoints:** Which endpoints belong in the core contract? Specific methods (`UaaEndpoint()`, `DopplerEndpoint()`, etc.) vs. a generic `Endpoint(name string) (string, error)` method that's extensible without interface changes.

3. **JSON-RPC error codes:** Define standard error codes for the core contract methods (e.g., `NOT_LOGGED_IN`, `TOKEN_EXPIRED`, `NO_TARGET`, `METHOD_NOT_AVAILABLE`).

4. **Embedded metadata schema:** Define the `CF_PLUGIN_METADATA:` JSON schema formally, including `schema_version` for evolution and all required/optional fields.

5. **Plugin lifecycle events:** Should uninstall/upgrade notifications be JSON-RPC methods? Currently the CLI sends `"CLI-MESSAGE-UNINSTALL"` as an arg to the plugin's `Run` method.

6. **Plugin configuration write access:** cf-targets-plugin bypasses the plugin API because there's no way to set/restore CLI configuration. Should the new interface provide `SetTarget(org, space)` or similar?

7. **Plugin-to-plugin communication:** No plugin currently depends on another plugin, but should the interface support this?

8. **Connection info passing:** How to pass TCP port and protocol to new-protocol plugins — environment variables (`CF_PLUGIN_PORT`, `CF_PLUGIN_PROTOCOL`) vs. other mechanism.

9. **Message serialization format:** Does the serialization format need to be fixed to JSON? The channel abstraction decouples transport from serialization — alternative formats like MessagePack, CBOR, or Protobuf could be supported alongside JSON-RPC. The `CF_PLUGIN_METADATA:` marker could declare the preferred format (e.g., `"serialization":"json"` or `"serialization":"msgpack"`). JSON is the most universally accessible, but binary formats offer better performance for high-volume data exchange.
