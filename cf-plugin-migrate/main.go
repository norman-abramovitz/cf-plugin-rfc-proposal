package main

import (
	"flag"
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
	if len(os.Args) < 2 || os.Args[1] == "-h" || os.Args[1] == "--help" || os.Args[1] == "help" {
		printUsage()
		if len(os.Args) < 2 {
			os.Exit(1)
		}
		os.Exit(0)
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
		fmt.Fprintf(os.Stderr, "Error: unknown command %q\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: cf-plugin-migrate <command> [options]")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  scan       Scan Go source for V2 plugin interface usage")
	fmt.Fprintln(os.Stderr, "  generate   Generate V2 compatibility wrapper from YAML config")
	fmt.Fprintln(os.Stderr, "  ast        Dump the AST for a Go source file (debug)")
	fmt.Fprintln(os.Stderr, "  version    Print version information")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Run 'cf-plugin-migrate <command> -h' for help on a specific command.")
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
	if len(os.Args) < 3 || os.Args[2] == "-h" || os.Args[2] == "--help" {
		fmt.Fprintln(os.Stderr, "Usage: cf-plugin-migrate ast <file.go>")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Dump the Go AST for a source file. Useful for debugging the scanner.")
		if len(os.Args) >= 3 {
			os.Exit(0)
		}
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
	fs := flag.NewFlagSet("generate", flag.ExitOnError)
	outputPath := fs.String("o", "", "output file path (default: v2compat_generated.go, use - for stdout)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: cf-plugin-migrate generate [options] [config.yml]")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Generate a V2Compat wrapper from a cf-plugin-migrate.yml config file.")
		fmt.Fprintln(os.Stderr, "The generated file implements plugin.CliConnection with V2 domain methods")
		fmt.Fprintln(os.Stderr, "backed by CAPI V3 via go-cfclient.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Options:")
		fs.PrintDefaults()
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Arguments:")
		fmt.Fprintln(os.Stderr, "  config.yml    Path to YAML config (default: cf-plugin-migrate.yml)")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Examples:")
		fmt.Fprintln(os.Stderr, "  cf-plugin-migrate generate")
		fmt.Fprintln(os.Stderr, "  cf-plugin-migrate generate -o v2compat.go my-config.yml")
		fmt.Fprintln(os.Stderr, "  cf-plugin-migrate generate -o - | head -20")
	}
	fs.Parse(os.Args[2:])

	configPath := "cf-plugin-migrate.yml"
	if fs.NArg() >= 1 {
		configPath = fs.Arg(0)
	}

	outPath := "v2compat_generated.go"
	if *outputPath != "" {
		outPath = *outputPath
	} else if fs.NArg() >= 2 {
		// Backward compat: generate [config] [output]
		outPath = fs.Arg(1)
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

	if outPath == "-" {
		os.Stdout.Write(output)
	} else {
		if err := os.WriteFile(outPath, output, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing %s: %v\n", outPath, err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Generated %s (%d bytes)\n", outPath, len(output))
	}
}

func runScan() {
	fs := flag.NewFlagSet("scan", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: cf-plugin-migrate scan [options] [patterns...]")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Scan Go source files for V2 plugin interface usage. Detects V2 domain")
		fmt.Fprintln(os.Stderr, "method calls, CliCommand calls, and field access patterns. Outputs a")
		fmt.Fprintln(os.Stderr, "cf-plugin-migrate.yml config to stdout and a human-readable summary")
		fmt.Fprintln(os.Stderr, "to stderr.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Options:")
		fs.PrintDefaults()
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Arguments:")
		fmt.Fprintln(os.Stderr, "  patterns    Go file patterns to scan (default: ./...)")
		fmt.Fprintln(os.Stderr, "              Excludes vendor/ directories and _test.go files.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Examples:")
		fmt.Fprintln(os.Stderr, "  cf-plugin-migrate scan ./...")
		fmt.Fprintln(os.Stderr, "  cf-plugin-migrate scan ./... > cf-plugin-migrate.yml")
		fmt.Fprintln(os.Stderr, "  cf-plugin-migrate scan ./cmd/ ./internal/")
	}
	fs.Parse(os.Args[2:])

	patterns := fs.Args()
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
