package cfconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func homeDir() (string, error) {
	if cfHome := os.Getenv("CF_HOME"); cfHome != "" {
		if _, err := os.Stat(cfHome); os.IsNotExist(err) {
			return "", fmt.Errorf("Error locating CF_HOME folder '%s'", cfHome)
		}
		return cfHome, nil
	}
	return userHomeDir(), nil
}

func userHomeDir() string {
	if runtime.GOOS == "windows" {
		home := os.Getenv("HOMEDRIVE") + os.Getenv("HOMEPATH")
		if home == "" {
			home = os.Getenv("USERPROFILE")
		}
		return home
	}
	return os.Getenv("HOME")
}

// DefaultFilePath returns the path to the CF CLI config file.
// Checks $CF_HOME first, falls back to $HOME/.cf/config.json.
// This is a drop-in replacement for cf/configuration/confighelpers.DefaultFilePath().
func DefaultFilePath() (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cf", "config.json"), nil
}

// PluginRepoDir is a function variable that returns the plugin repo directory.
// Checks $CF_PLUGIN_HOME first, falls back to the CF home directory.
// This is a drop-in replacement for cf/configuration/confighelpers.PluginRepoDir.
var PluginRepoDir = func() string {
	if pluginHome := os.Getenv("CF_PLUGIN_HOME"); pluginHome != "" {
		return pluginHome
	}
	home, _ := homeDir()
	return home
}
