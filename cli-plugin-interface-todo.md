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
| `plugin/plugin.go` | `Plugin`, `CliConnection`, `PluginMetadata`, `VersionType`, `Command`, `Usage` |
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

    // CF Client (recommended for CAPI V3 access)
    CfClient() (*cfclient.Client, error)
}
```

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

- [ ] Decide: Does `CfClient()` live in the core interface or a separate helper package?
  - Pro of core: eliminates boilerplate in every plugin
  - Con of core: adds `go-cfclient` as a dependency to the interface module
  - Possible compromise: provide `CfClient()` in a companion package (e.g., `cli-plugin-interface/cfclient`) that plugins opt into

### 2.2 New `Plugin` interface

- [ ] Update the `Plugin` interface to accept `PluginContext`:

```go
type Plugin interface {
    Run(ctx PluginContext, args []string)
    GetMetadata() PluginMetadata
}
```

- [ ] Update `PluginMetadata` with full semver support:

```go
type PluginVersion struct {
    Major      int
    Minor      int
    Build      int
    PreRelease string
    BuildMeta  string
}
```

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
- [ ] `CfClient()` — this is the biggest change. Options:
  - **Option A: Server-side construction** — the CLI builds a `go-cfclient` client using its own config and passes it over RPC. Problem: `go-cfclient.Client` is not RPC-serializable.
  - **Option B: Client-side construction** — the plugin constructs the client locally using `AccessToken()` + `ApiEndpoint()` + `IsSSLDisabled()`. The `CfClient()` method on the RPC client would do this internally without an actual RPC call.
  - **Option C: Helper package** — provide a `NewCfClient(ctx PluginContext)` function that plugins call. No RPC change needed.
  - **Recommendation: Option B or C** — both avoid RPC serialization issues. Option C is cleanest (no interface change).
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

## Phase 4: Plugin Help Integration

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

**What plugins CANNOT provide** (built-in commands get these from Go struct tags):
- Examples
- Related commands / "SEE ALSO"
- Environment variables
- Flag default values, argument types, required/optional flags
- Long-form description beyond the single-line `HelpText`
- Category grouping within the plugin section

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

- [ ] Add optional fields to the `Command` struct:

```go
type Command struct {
    Name         string
    Alias        string
    HelpText     string          // Short description (existing)
    Description  string          // Long-form description (new)
    Examples     string          // Usage examples (new)
    RelatedCmds  []string        // See also (new)
    UsageDetails Usage
}
```

- [ ] Update `ConvertPluginToCommandInfo()` in `command/common/internal/help_display.go` to populate `Examples` and `RelatedCommands` fields
- [ ] These new fields are optional — existing plugins that don't set them continue to work

#### 4.3 Enrich `Usage.Options` for richer flag help

- [ ] Consider replacing `Options map[string]string` with a structured type:

```go
type FlagDetail struct {
    Description string
    Default     string   // default value
    Required    bool
    HasArg      bool     // whether the flag takes an argument
}
type Usage struct {
    Usage   string
    Options map[string]FlagDetail   // BREAKING — needs migration path
}
```

- [ ] Alternatively, keep the `map[string]string` for backward compatibility and add a parallel `OptionsV2 map[string]FlagDetail` field
- [ ] This is lower priority — the current string-only options work adequately

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

- [ ] Ensure `~/.cf/plugins/config.json` stores any new metadata fields (`Description`, `Examples`, `RelatedCmds`)
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

### 6.2 Migration documentation

- [ ] Write a migration guide with before/after code examples
- [ ] Migrate at least one reference plugin (e.g., ocf-scheduler-cf-plugin) as a worked example
- [ ] Document common migration patterns:
  - `GetApp(name)` → `cfclient.Applications.ListAll()` with name+space filter
  - `GetService(name)` → `cfclient.ServiceInstances.Single()` with name+space filter
  - `CliCommandWithoutTerminalOutput("curl", path)` → `cfclient` or `http.Client`
  - Deriving service URLs from `ApiEndpoint()`

### 6.3 Community communication

- [ ] Post RFC to `cloudfoundry/community` as a PR
- [ ] Present at CF community call
- [ ] Notify known plugin maintainers:
  - SAP (@s-yonkov-yonkov, @silvestre) — MTA, DefaultEnv, html5, java
  - Pivotal/Broadcom — metric-registrar, service-instance-logs, spring-cloud-services, mysql-cli
  - Swisscom (@jcvrabo) — appcloud
  - Community — OCF Scheduler, cf-targets, Rabobank, cf-lookup-route

---

## Open Questions

1. **Module naming:** `code.cloudfoundry.org/cli-plugin-interface` vs `code.cloudfoundry.org/cli-plugin-api` vs `code.cloudfoundry.org/cli/v9/plugin` (stay in-tree)?

2. **RPC protocol versioning:** The current RPC uses Go's `net/rpc` with `encoding/gob`. Should the new interface introduce protocol versioning or capability negotiation? Or is that deferred to a future gRPC-based plugin model?

3. **`CfClient()` placement:** Core interface vs. companion package vs. standalone helper function?

4. **`CliCommand` replacement:** Some plugins (mysql-cli, metric-registrar) use `CliCommand` to orchestrate multi-step workflows (push, bind, restage). Is `CfClient()` sufficient, or do they need a workflow API?

5. **Plugin configuration write access:** cf-targets-plugin bypasses the plugin API because there's no way to set/restore CLI configuration. Should the new interface provide `SetTarget(org, space)` or similar?

6. **Plugin-to-plugin communication:** No plugin currently depends on another plugin, but should the interface support this?
