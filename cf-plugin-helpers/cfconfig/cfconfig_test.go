package cfconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultFilePath(t *testing.T) {
	path := DefaultFilePath()
	if path == "" {
		t.Fatal("expected non-empty path")
	}
	if !strings.HasSuffix(path, filepath.Join(".cf", "config.json")) {
		t.Errorf("expected path ending in .cf/config.json, got %q", path)
	}
}

func TestDefaultFilePathWithCFHome(t *testing.T) {
	t.Setenv("CF_HOME", "/tmp/test-cf-home")
	path := DefaultFilePath()
	want := filepath.Join("/tmp/test-cf-home", ".cf", "config.json")
	if path != want {
		t.Errorf("expected %q, got %q", want, path)
	}
}

func TestPluginRepoDir(t *testing.T) {
	path := PluginRepoDir()
	if path == "" {
		t.Fatal("expected non-empty path")
	}
	if !strings.HasSuffix(path, filepath.Join(".cf", "plugins")) {
		t.Errorf("expected path ending in .cf/plugins, got %q", path)
	}
}

func TestPluginRepoDirWithCFHome(t *testing.T) {
	t.Setenv("CF_HOME", "/tmp/test-cf-home")
	path := PluginRepoDir()
	want := filepath.Join("/tmp/test-cf-home", ".cf", "plugins")
	if path != want {
		t.Errorf("expected %q, got %q", want, path)
	}
}

func TestDefaultFilePathNoHome(t *testing.T) {
	// Unset both CF_HOME and HOME to test fallback
	t.Setenv("CF_HOME", "")
	orig := os.Getenv("HOME")
	t.Setenv("HOME", "")
	defer os.Setenv("HOME", orig)
	// Should not panic — returns empty string
	_ = DefaultFilePath()
}
