package scanner

import "strings"

// InternalImportReplacement defines the replacement guidance for a CLI internal package.
type InternalImportReplacement struct {
	Replacement string // cf-plugin-helpers import path
	Note        string // migration guidance
}

// InternalImportReplacements maps known CLI internal import paths to replacement suggestions.
// Three import path variants are included:
//   - Old github.com path (github.com/cloudfoundry/cli/cf/...) — pre-modules plugins
//   - Module path (code.cloudfoundry.org/cli/cf/...) — v7-era plugins
//   - v8+ path (code.cloudfoundry.org/cli/v8/cf/...) — v8-era plugins
var InternalImportReplacements = map[string]InternalImportReplacement{
	// cf/configuration/confighelpers
	"github.com/cloudfoundry/cli/cf/configuration/confighelpers": {
		Replacement: "code.cloudfoundry.org/cf-plugin-helpers/cfconfig",
		Note:        "DefaultFilePath(), PluginRepoDir() — import swap, no code changes",
	},
	"code.cloudfoundry.org/cli/cf/configuration/confighelpers": {
		Replacement: "code.cloudfoundry.org/cf-plugin-helpers/cfconfig",
		Note:        "DefaultFilePath(), PluginRepoDir() — import swap, no code changes",
	},
	"code.cloudfoundry.org/cli/v8/cf/configuration/confighelpers": {
		Replacement: "code.cloudfoundry.org/cf-plugin-helpers/cfconfig",
		Note:        "DefaultFilePath(), PluginRepoDir() — import swap, no code changes",
	},

	// cf/trace
	"github.com/cloudfoundry/cli/cf/trace": {
		Replacement: "code.cloudfoundry.org/cf-plugin-helpers/cftrace",
		Note:        "Printer, NewLogger(), NewWriterPrinter() — import swap, no code changes",
	},
	"code.cloudfoundry.org/cli/cf/trace": {
		Replacement: "code.cloudfoundry.org/cf-plugin-helpers/cftrace",
		Note:        "Printer, NewLogger(), NewWriterPrinter() — import swap, no code changes",
	},
	"code.cloudfoundry.org/cli/v8/cf/trace": {
		Replacement: "code.cloudfoundry.org/cf-plugin-helpers/cftrace",
		Note:        "Printer, NewLogger(), NewWriterPrinter() — import swap, no code changes",
	},

	// cf/terminal
	"github.com/cloudfoundry/cli/cf/terminal": {
		Replacement: "code.cloudfoundry.org/cf-plugin-helpers/cfui",
		Note:        "UI, TeePrinter, NewUI, color functions — import swap for Pattern B (UI bootstrap); Pattern A (full framework, 16+ files) may need additional work",
	},
	"code.cloudfoundry.org/cli/cf/terminal": {
		Replacement: "code.cloudfoundry.org/cf-plugin-helpers/cfui",
		Note:        "UI, TeePrinter, NewUI, color functions — import swap for Pattern B (UI bootstrap); Pattern A (full framework, 16+ files) may need additional work",
	},
	"code.cloudfoundry.org/cli/v8/cf/terminal": {
		Replacement: "code.cloudfoundry.org/cf-plugin-helpers/cfui",
		Note:        "UI, TeePrinter, NewUI, color functions — import swap for Pattern B (UI bootstrap); Pattern A (full framework, 16+ files) may need additional work",
	},

	// cf/formatters
	"github.com/cloudfoundry/cli/cf/formatters": {
		Replacement: "code.cloudfoundry.org/cf-plugin-helpers/cfformat",
		Note:        "ByteSize() — import swap, no code changes",
	},
	"code.cloudfoundry.org/cli/cf/formatters": {
		Replacement: "code.cloudfoundry.org/cf-plugin-helpers/cfformat",
		Note:        "ByteSize() — import swap, no code changes",
	},
	"code.cloudfoundry.org/cli/v8/cf/formatters": {
		Replacement: "code.cloudfoundry.org/cf-plugin-helpers/cfformat",
		Note:        "ByteSize() — import swap, no code changes",
	},

	// cf/i18n — transitive dependency of cf/terminal
	"github.com/cloudfoundry/cli/cf/i18n": {
		Replacement: "",
		Note:        "Transitive dependency of cf/terminal — removing terminal eliminates this import",
	},
	"code.cloudfoundry.org/cli/cf/i18n": {
		Replacement: "",
		Note:        "Transitive dependency of cf/terminal — removing terminal eliminates this import",
	},
	"code.cloudfoundry.org/cli/v8/cf/i18n": {
		Replacement: "",
		Note:        "Transitive dependency of cf/terminal — removing terminal eliminates this import",
	},

	// cf/flags
	"github.com/cloudfoundry/cli/cf/flags": {
		Replacement: "",
		Note:        "Replace with stdlib flag or pflag",
	},
	"code.cloudfoundry.org/cli/cf/flags": {
		Replacement: "",
		Note:        "Replace with stdlib flag or pflag",
	},
	"code.cloudfoundry.org/cli/v8/cf/flags": {
		Replacement: "",
		Note:        "Replace with stdlib flag or pflag",
	},

	// cf/configuration + coreconfig — hard case
	"github.com/cloudfoundry/cli/cf/configuration": {
		Replacement: "",
		Note:        "Direct config file access — no drop-in replacement. See RFC for options: copy code, keep import, or request CLI team support",
	},
	"code.cloudfoundry.org/cli/cf/configuration": {
		Replacement: "",
		Note:        "Direct config file access — no drop-in replacement. See RFC for options: copy code, keep import, or request CLI team support",
	},
	"code.cloudfoundry.org/cli/v8/cf/configuration": {
		Replacement: "",
		Note:        "Direct config file access — no drop-in replacement. See RFC for options",
	},
	"github.com/cloudfoundry/cli/cf/configuration/coreconfig": {
		Replacement: "",
		Note:        "Direct config file access — no drop-in replacement. See RFC for options",
	},
	"code.cloudfoundry.org/cli/cf/configuration/coreconfig": {
		Replacement: "",
		Note:        "Direct config file access — no drop-in replacement. See RFC for options",
	},
	"code.cloudfoundry.org/cli/v8/cf/configuration/coreconfig": {
		Replacement: "",
		Note:        "Direct config file access — no drop-in replacement. See RFC for options",
	},

	// util/configv3 — transitive dependency of util/ui in mysql-cli-plugin
	"code.cloudfoundry.org/cli/util/configv3": {
		Replacement: "",
		Note:        "Transitive dependency of util/ui — replace the confirmation prompt with fmt.Print + bufio.Scanner to eliminate both imports",
	},

	// util/ui
	"code.cloudfoundry.org/cli/util/ui": {
		Replacement: "",
		Note:        "Replace DisplayBoolPrompt() with fmt.Print + bufio.Scanner (~10 lines)",
	},
}

// isAllowedImport returns true if the import is part of the intended public plugin contract.
func isAllowedImport(importPath string) bool {
	// Exact matches for plugin and plugin/models (with or without v8 prefix)
	allowed := []string{
		"github.com/cloudfoundry/cli/plugin",
		"github.com/cloudfoundry/cli/plugin/models",
		"code.cloudfoundry.org/cli/plugin",
		"code.cloudfoundry.org/cli/plugin/models",
		"code.cloudfoundry.org/cli/v8/plugin",
		"code.cloudfoundry.org/cli/v8/plugin/models",
	}
	for _, a := range allowed {
		if importPath == a {
			return true
		}
	}
	return false
}

// isCLIImport returns true if the import path is from the CF CLI module.
func isCLIImport(importPath string) bool {
	return strings.HasPrefix(importPath, "code.cloudfoundry.org/cli/") ||
		strings.HasPrefix(importPath, "code.cloudfoundry.org/cli/v8/") ||
		// Old github.com path (pre-modules plugins like html5-apps-repo, list-services)
		strings.HasPrefix(importPath, "github.com/cloudfoundry/cli/")
}
