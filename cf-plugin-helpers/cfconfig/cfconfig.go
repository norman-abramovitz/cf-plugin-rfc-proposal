package cfconfig

import (
	"os"
	"path/filepath"
)

// DefaultFilePath returns the path to the CF CLI config file.
// Checks $CF_HOME first, falls back to $HOME/.cf/config.json.
// This is a drop-in replacement for cf/configuration/confighelpers.DefaultFilePath().
func DefaultFilePath() string {
	if cfHome := os.Getenv("CF_HOME"); cfHome != "" {
		return filepath.Join(cfHome, ".cf", "config.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cf", "config.json")
}

// PluginRepoDir returns the path to the CF CLI plugin repo directory.
// Checks $CF_HOME first, falls back to $HOME/.cf/plugins.
// This is a drop-in replacement for cf/configuration/confighelpers.PluginRepoDir().
func PluginRepoDir() string {
	if cfHome := os.Getenv("CF_HOME"); cfHome != "" {
		return filepath.Join(cfHome, ".cf", "plugins")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cf", "plugins")
}
