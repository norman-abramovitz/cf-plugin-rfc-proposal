package generator

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGoldenFiles verifies that the generator produces byte-identical output
// for known YAML configs. Each .yml file in testdata/ has a corresponding
// .golden file with the expected output. Run with -update to regenerate:
//
//	go test -run TestGoldenFiles -update
//
// The golden files are checked into version control so regressions are caught
// by CI without needing the -update flag.
var update = flag.Bool("update", false, "update golden files")

func TestGoldenFiles(t *testing.T) {
	ymls, err := filepath.Glob("testdata/*_plugin.yml")
	if err != nil {
		t.Fatal(err)
	}
	if len(ymls) == 0 {
		t.Fatal("no testdata/*_plugin.yml files found")
	}

	for _, yml := range ymls {
		name := strings.TrimSuffix(filepath.Base(yml), ".yml")
		goldenPath := filepath.Join("testdata", name+".golden")

		t.Run(name, func(t *testing.T) {
			config, err := LoadConfig(yml)
			if err != nil {
				t.Fatalf("loading config %s: %v", yml, err)
			}

			got, err := Generate(config)
			if err != nil {
				t.Fatalf("generating from %s: %v", yml, err)
			}

			if *update {
				if err := os.WriteFile(goldenPath, got, 0644); err != nil {
					t.Fatalf("updating golden file %s: %v", goldenPath, err)
				}
				t.Logf("updated %s", goldenPath)
				return
			}

			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("reading golden file %s: %v\nRun with -update to generate it.", goldenPath, err)
			}

			if string(got) != string(want) {
				t.Errorf("output mismatch for %s\n\nRun with -update to regenerate golden files.\n\n%s",
					yml, diff(string(want), string(got)))
			}
		})
	}
}

// diff returns a simple line-by-line diff showing the first difference.
func diff(want, got string) string {
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")

	maxLines := len(wantLines)
	if len(gotLines) > maxLines {
		maxLines = len(gotLines)
	}

	for i := 0; i < maxLines; i++ {
		var wl, gl string
		if i < len(wantLines) {
			wl = wantLines[i]
		}
		if i < len(gotLines) {
			gl = gotLines[i]
		}
		if wl != gl {
			return strings.Join([]string{
				"First difference at line " + itoa(i+1) + ":",
				"  want: " + wl,
				"  got:  " + gl,
			}, "\n")
		}
	}
	return "(files differ in trailing content)"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
