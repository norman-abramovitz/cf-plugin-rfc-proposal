# TODO — CLI Plugin Interface V2 RFC

## RFC Document

### Research & Analysis (Completed)

- [x] Survey actively maintained CF CLI plugins (18 plugins analyzed — see [plugin-survey.md](plugin-survey.md))
- [x] Document how plugins interact with CF outside the plugin interface (go-cfclient, cf curl, direct HTTP, exec bypass, file I/O)
- [x] Analyze CF CLI plugin internals (`plugin/`, `plugin/models/`, `plugin/rpc/`) — see [cli-plugin-interface-todo.md](cli-plugin-interface-todo.md)
- [x] Analyze plugin help system integration (`command/common/help_command.go`, `GetMetadata().Commands`)
- [x] Draft the list of Core Plugin API methods (Session, Endpoint, Context)
- [x] Define concrete Go interface types (`PluginContext`, `OrgContext`, `SpaceContext`, `PluginVersion`, `Command`, `Usage`, `FlagDefinition`)
- [x] Draft migration timeline (Phases 1–4 in cli-plugin-interface-todo.md)
- [x] Add help system findings and proposals to RFC
- [x] Analyze `VersionType` limitations (Build misnomer, no prerelease/build metadata, plugin workarounds)
- [x] Analyze `Options map[string]string` limitations (unordered, no long/short pairing, no defaults/grouping, `ConvertPluginToCommandInfo` processing)
- [x] Review [CF CLI Help Guidelines](https://github.com/cloudfoundry/cli/wiki/CF-CLI-Help-Guidelines) — standard help sections (NAME, USAGE/[docopt](http://docopt.org/), WARNING, EXAMPLE, TIP, ALIAS, OPTIONS, SEE ALSO)
- [x] Review [CF CLI Style Guide](https://github.com/cloudfoundry/cli/wiki/CF-CLI-Style-Guide) — command naming, fail-fast validation, output formatting, color, flag design
- [x] Review [CLI Product-Specific Style Guide](https://github.com/cloudfoundry/cli/wiki/CLI-Product-Specific-Style-Guide) — error patterns, TIPs, confirmation prompts, idempotency
- [x] Review [Version Switching Guide](https://github.com/cloudfoundry/cli/wiki/Version-Switching-Guide) — interface evolution considerations
- [x] Add version and flag metadata findings to RFC, CLI TODO, and plugin survey
- [x] Add wiki guide references to RFC, CLI TODO, and plugin survey
- [x] Define interface evolution strategy (backward-compatible structs, additive RPC, capability discovery, deprecation signaling)
- [x] Add `Warning`, `Tip` fields to `Command` struct per Help Guidelines
- [x] Add `Group` field to `FlagDefinition` for organized flag display
- [x] Rename `PluginVersion.Build` → `Patch` for SemVer correctness; add `PreRelease`, `BuildMeta`, `String()`

### Decisions Needed

- [ ] Decide: Should `CfClient()` be part of the core contract or a separate helper package?
- [ ] Decide: Which additional endpoints to include (UAA, Doppler, Routing API, CredHub)
- [ ] Decide: Should `CliCommand`/`CliCommandWithoutTerminalOutput` be kept for any transition use?
- [ ] Add error handling and edge case guidance (expired tokens, no target, etc.)

### Stakeholder Review

- [ ] Review RFC draft with CLI maintainers (@a-b, @gururajsh, @anujc25, @moleske)
- [ ] Incorporate feedback from @beyhan and @silvestre on minimal API surface
- [ ] Incorporate feedback from @s-yonkov-yonkov (MTA plugin) on backward compatibility
- [ ] Incorporate feedback from @jcvrabo on go-cfclient integration and plugin repo management
- [ ] Incorporate feedback from @parttimenerd (cf-java-plugin) on dependency updates
- [ ] Review migration timeline (Phases 1–4) for feasibility with CLI team

## Reference Implementation

- [ ] Create a standalone Go module for the new plugin interface
- [ ] Implement `PluginContext` interface in the CLI
- [ ] Build a reference plugin demonstrating the new pattern
- [ ] Migrate ocf-scheduler-cf-plugin as a real-world migration example
- [ ] Document migration steps from legacy interface to new interface

## Plugin Repository

- [ ] Draft plugin repository deprecation policy
- [ ] Define metadata schema for plugin compatibility (min CLI version, min CAPI version)
- [ ] Propose plugin repository format changes (version compatibility, deprecation attributes)

## Community Process

- [ ] Post RFC draft to cloudfoundry/community as a PR
- [ ] Mention @cloudfoundry/toc and relevant working groups
- [ ] Present at CF community call
- [ ] Collect feedback during public discussion period
- [ ] Request Final Comment Period (FCP)

## Future RFCs (Out of Scope)

- [ ] Polyglot plugin support (gRPC-based plugin model)
- [ ] GitHub-style plugin distribution and trust model
- [ ] CLI adoption of go-cfclient internally
- [ ] Standard option parsing framework for plugins
