package cfconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultFilePath(t *testing.T) {
	path, err := DefaultFilePath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path == "" {
		t.Fatal("expected non-empty path")
	}
	if !strings.HasSuffix(path, filepath.Join(".cf", "config.json")) {
		t.Errorf("expected path ending in .cf/config.json, got %q", path)
	}
}

func TestDefaultFilePathWithCFHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CF_HOME", dir)
	path, err := DefaultFilePath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(dir, ".cf", "config.json")
	if path != want {
		t.Errorf("expected %q, got %q", want, path)
	}
}

func TestDefaultFilePathWithCFHomeNotExist(t *testing.T) {
	t.Setenv("CF_HOME", "/nonexistent/path/that/should/not/exist")
	_, err := DefaultFilePath()
	if err == nil {
		t.Fatal("expected error for nonexistent CF_HOME")
	}
}

func TestPluginRepoDir(t *testing.T) {
	t.Setenv("CF_PLUGIN_HOME", "")
	path := PluginRepoDir()
	if path == "" {
		t.Fatal("expected non-empty path")
	}
}

func TestPluginRepoDirWithCFPluginHome(t *testing.T) {
	t.Setenv("CF_PLUGIN_HOME", "/tmp/test-plugin-home")
	path := PluginRepoDir()
	if path != "/tmp/test-plugin-home" {
		t.Errorf("expected /tmp/test-plugin-home, got %q", path)
	}
}

func TestDefaultFilePathNoHome(t *testing.T) {
	t.Setenv("CF_HOME", "")
	orig := os.Getenv("HOME")
	t.Setenv("HOME", "")
	defer os.Setenv("HOME", orig)
	// Should not panic — returns empty path
	path, _ := DefaultFilePath()
	_ = path
}
