# CF CLI Plugin Interface V3 — RFC Proposal

This repository contains an RFC proposal to modernize the Cloud Foundry CLI plugin interface.

## Background

The current CF CLI plugin interface has not been updated in years, depends on
archived packages, and tightly couples plugins to CAPI V2 domain models. Multiple
active plugin maintainers have independently migrated to a pattern where the CLI
provides only authentication and context while plugins interact with CAPI V3
directly. This RFC formalizes that pattern as the new plugin interface.

## Documents

- [RFC Draft: CLI Plugin Interface V3](rfc-draft-cli-plugin-interface-v3-detailed.md) — Full technical specification
- [RFC Draft: Transitional Migration](rfc-draft-plugin-transitional-migration.md) — Community-submission RFC (concise)
- [Transitional Migration — Detailed Design](rfc-draft-plugin-transitional-migration-detailed.md) — Full technical specification with code examples and field mappings
- [Plugin Survey](plugin-survey.md) — Analysis of 18 actively maintained CF CLI plugins
- [CLI Plugin Interface TODO](cli-plugin-interface-todo.md) — What needs to change in the CLI codebase
- [TODO](TODO.md) — Overall RFC work items

## Related

- [cloudfoundry/cli#3621 — New Plugin Interface](https://github.com/cloudfoundry/cli/issues/3621)
- [app-autoscaler-cli-plugin PR #132](https://github.com/cloudfoundry/app-autoscaler-cli-plugin/pull/132)
- [go-cfclient](https://github.com/cloudfoundry/go-cfclient)
