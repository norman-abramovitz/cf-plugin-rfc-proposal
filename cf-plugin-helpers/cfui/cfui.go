package cfui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"code.cloudfoundry.org/cf-plugin-helpers/cftrace"
)

// UI provides formatted terminal output for CF CLI plugins.
// This matches cf/terminal.UI for the Pattern B subset.
type UI interface {
	Say(message string, args ...interface{})
	Warn(message string, args ...interface{})
	Failed(message string, args ...interface{})
	Ok()
	Table(headers []string) UITable
}

// UITable supports row-based table output.
type UITable interface {
	Add(row ...string)
	Print()
}

// TeePrinter captures output while also printing it.
type TeePrinter struct {
	writer io.Writer
	buf    strings.Builder
}

// NewTeePrinter creates a TeePrinter that writes to w.
func NewTeePrinter(w io.Writer) *TeePrinter {
	return &TeePrinter{writer: w}
}

func (p *TeePrinter) Write(data []byte) (int, error) {
	p.buf.Write(data)
	return p.writer.Write(data)
}

// String returns all captured output.
func (p *TeePrinter) String() string {
	return p.buf.String()
}

// Color control — matches cf/terminal globals.
var UserAskedForColors string

// colorEnabled returns whether ANSI colors should be used.
func colorEnabled() bool {
	switch strings.ToLower(UserAskedForColors) {
	case "true":
		return true
	case "false":
		return false
	}
	// Check CF_COLOR env var
	switch strings.ToLower(os.Getenv("CF_COLOR")) {
	case "true":
		return true
	case "false":
		return false
	}
	// Default: enabled if stdout is a terminal
	return true
}

// InitColorSupport initializes color support based on CF_COLOR env var.
func InitColorSupport() {
	if cfColor := os.Getenv("CF_COLOR"); cfColor != "" {
		UserAskedForColors = cfColor
	}
}

// Color functions matching cf/terminal signatures.

func EntityNameColor(message string) string {
	if !colorEnabled() {
		return message
	}
	return "\033[36m" + message + "\033[0m" // cyan
}

func CommandColor(message string) string {
	if !colorEnabled() {
		return message
	}
	return "\033[1;33m" + message + "\033[0m" // bold yellow
}

func FailureColor(message string) string {
	if !colorEnabled() {
		return message
	}
	return "\033[1;31m" + message + "\033[0m" // bold red
}

// NewUI creates a UI instance.
func NewUI(in io.Reader, out io.Writer, printer *TeePrinter, logger cftrace.Printer) UI {
	w := out
	if printer != nil {
		w = printer
	}
	return &ui{in: in, out: w}
}

type ui struct {
	in  io.Reader
	out io.Writer
}

func (u *ui) Say(message string, args ...interface{}) {
	if len(args) > 0 {
		fmt.Fprintf(u.out, message+"\n", args...)
	} else {
		fmt.Fprintln(u.out, message)
	}
}

func (u *ui) Warn(message string, args ...interface{}) {
	msg := message
	if len(args) > 0 {
		msg = fmt.Sprintf(message, args...)
	}
	if colorEnabled() {
		fmt.Fprintln(u.out, "\033[33m"+msg+"\033[0m") // yellow
	} else {
		fmt.Fprintln(u.out, msg)
	}
}

func (u *ui) Failed(message string, args ...interface{}) {
	msg := message
	if len(args) > 0 {
		msg = fmt.Sprintf(message, args...)
	}
	fmt.Fprintln(u.out, FailureColor("FAILED"))
	fmt.Fprintln(u.out, msg)
}

func (u *ui) Ok() {
	if colorEnabled() {
		fmt.Fprintln(u.out, "\033[32mOK\033[0m") // green
	} else {
		fmt.Fprintln(u.out, "OK")
	}
}

func (u *ui) Table(headers []string) UITable {
	return &uiTable{
		out:     u.out,
		headers: headers,
	}
}

type uiTable struct {
	out     io.Writer
	headers []string
	rows    [][]string
}

func (t *uiTable) Add(row ...string) {
	t.rows = append(t.rows, row)
}

func (t *uiTable) Print() {
	tw := tabwriter.NewWriter(t.out, 0, 4, 3, ' ', 0)
	fmt.Fprintln(tw, strings.Join(t.headers, "\t"))
	for _, row := range t.rows {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	tw.Flush()
}
