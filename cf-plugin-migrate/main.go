package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"runtime/debug"
	"strings"

	"cf-plugin-migrate/generator"
	"cf-plugin-migrate/scanner"
)

// version and buildDate are set via ldflags at build time.
// For local builds, they remain "dev" and "unknown".
var (
	version   = "dev"
	buildDate = "unknown"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: cf-plugin-migrate <scan|generate|ast> [args...]")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Commands:")
		fmt.Fprintln(os.Stderr, "  scan [./...]       Scan Go source for V2 domain method calls")
		fmt.Fprintln(os.Stderr, "  generate           Generate V2 compatibility wrappers from YAML")
		fmt.Fprintln(os.Stderr, "  ast <file.go>      Dump the AST for a Go source file")
		fmt.Fprintln(os.Stderr, "  version            Print version information")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "scan":
		runScan()
	case "ast":
		runAstDump()
	case "generate":
		runGenerate()
	case "version":
		printVersion()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

func printVersion() {
	var b strings.Builder
	fmt.Fprintf(&b, "cf-plugin-migrate %s\n", version)

	if buildDate != "unknown" {
		fmt.Fprintf(&b, "  Build date:    %s\n", buildDate)
	}

	if info, ok := debug.ReadBuildInfo(); ok {
		fmt.Fprintf(&b, "  Go version:    %s\n", info.GoVersion)

		if info.Main.Path != "" {
			fmt.Fprintf(&b, "  Module:        %s\n", info.Main.Path)
		}

		var revision, revTime string
		var modified bool
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				revision = s.Value
			case "vcs.time":
				revTime = s.Value
			case "vcs.modified":
				modified = s.Value == "true"
			}
		}

		if revision != "" {
			short := revision
			if len(short) > 12 {
				short = short[:12]
			}
			fmt.Fprintf(&b, "  Commit:        %s (%s)\n", short, revision)
		}
		if revTime != "" {
			fmt.Fprintf(&b, "  Commit date:   %s\n", revTime)
		}
		if revision != "" {
			state := "clean"
			if modified {
				state = "dirty"
			}
			fmt.Fprintf(&b, "  Repo state:    %s\n", state)
		}
	}

	fmt.Print(b.String())
}

func runAstDump() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: cf-plugin-migrate ast <file.go>")
		os.Exit(1)
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, os.Args[2], nil, parser.ParseComments)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing %s: %v\n", os.Args[2], err)
		os.Exit(1)
	}

	if err := ast.Print(fset, f); err != nil {
		fmt.Fprintf(os.Stderr, "Error printing AST: %v\n", err)
		os.Exit(1)
	}
}

func runGenerate() {
	configPath := "cf-plugin-migrate.yml"
	outputPath := "v2compat_generated.go"

	// Allow overriding paths via args: generate [config] [output]
	args := os.Args[2:]
	if len(args) >= 1 {
		configPath = args[0]
	}
	if len(args) >= 2 {
		outputPath = args[1]
	}

	config, err := generator.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	output, err := generator.Generate(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if outputPath == "-" {
		os.Stdout.Write(output)
	} else {
		if err := os.WriteFile(outputPath, output, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing %s: %v\n", outputPath, err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Generated %s (%d bytes)\n", outputPath, len(output))
	}
}

func runScan() {
	patterns := os.Args[2:]
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}

	result, err := scanner.Scan(patterns)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Print human-readable summary to stderr
	result.WriteSummary(os.Stderr)

	// Write YAML to stdout
	if err := result.WriteYAML(os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing YAML: %v\n", err)
		os.Exit(1)
	}
}
