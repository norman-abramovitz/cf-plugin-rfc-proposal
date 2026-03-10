package cftrace

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Printer is the interface for trace output.
// This matches cf/trace.Printer exactly.
type Printer interface {
	Print(v ...interface{})
	Printf(format string, v ...interface{})
	Println(v ...interface{})
	WritesToConsole() bool
}

// NewLogger returns a Printer that conditionally logs based on CF_TRACE.
// boolsOrPaths are checked in order: "true"/"false" toggle console output,
// any other string is treated as a file path for trace output.
// This is a drop-in replacement for cf/trace.NewLogger().
func NewLogger(writer io.Writer, verbose bool, boolsOrPaths ...string) Printer {
	// Check CF_TRACE env var
	cfTrace := os.Getenv("CF_TRACE")

	writesToConsole := verbose
	var fileWriter io.Writer

	// Process explicit args first, then fall back to CF_TRACE
	allPaths := append(boolsOrPaths, cfTrace)
	for _, val := range allPaths {
		val = strings.TrimSpace(val)
		switch strings.ToLower(val) {
		case "true":
			writesToConsole = true
		case "false", "":
			// do nothing
		default:
			// Treat as file path
			if f, err := os.OpenFile(val, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
				fileWriter = f
			}
		}
	}

	var writers []io.Writer
	if writesToConsole {
		writers = append(writers, writer)
	}
	if fileWriter != nil {
		writers = append(writers, fileWriter)
	}

	if len(writers) == 0 {
		return &nullPrinter{}
	}

	combined := io.MultiWriter(writers...)
	return &writerPrinter{writer: combined, console: writesToConsole}
}

// NewWriterPrinter returns a Printer that writes to the given writer.
// This is a drop-in replacement for cf/trace.NewWriterPrinter().
func NewWriterPrinter(writer io.Writer, writesToConsole bool) Printer {
	return &writerPrinter{writer: writer, console: writesToConsole}
}

type writerPrinter struct {
	writer  io.Writer
	console bool
}

func (p *writerPrinter) Print(v ...interface{}) {
	fmt.Fprint(p.writer, v...)
}

func (p *writerPrinter) Printf(format string, v ...interface{}) {
	fmt.Fprintf(p.writer, format, v...)
}

func (p *writerPrinter) Println(v ...interface{}) {
	fmt.Fprintln(p.writer, v...)
}

func (p *writerPrinter) WritesToConsole() bool {
	return p.console
}

type nullPrinter struct{}

func (p *nullPrinter) Print(v ...interface{})                 {}
func (p *nullPrinter) Printf(format string, v ...interface{}) {}
func (p *nullPrinter) Println(v ...interface{})               {}
func (p *nullPrinter) WritesToConsole() bool                  { return false }
