package cfui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"code.cloudfoundry.org/cf-plugin-helpers/cftrace"
)

// Printer matches cf/terminal.Printer — the low-level print interface.
type Printer interface {
	Print(a ...interface{}) (n int, err error)
	Printf(format string, a ...interface{}) (n int, err error)
	Println(a ...interface{}) (n int, err error)
}

// UI provides formatted terminal output for CF CLI plugins.
// This matches cf/terminal.UI so plugins can import-swap without code changes.
type UI interface {
	PrintPaginator(rows []string, err error)
	Say(message string, args ...interface{})
	PrintCapturingNoOutput(message string, args ...interface{})
	Warn(message string, args ...interface{})
	Ask(prompt string) (answer string)
	AskForPassword(prompt string) (answer string)
	Confirm(message string) bool
	ConfirmDelete(modelType, modelName string) bool
	ConfirmDeleteWithAssociations(modelType, modelName string) bool
	Ok()
	Failed(message string, args ...interface{})
	LoadingIndication()
	Table(headers []string) *UITable
	Writer() io.Writer
}

// TeePrinter captures output while also printing it.
// Implements the Printer interface.
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

// Print implements Printer. Output is captured and written.
func (p *TeePrinter) Print(a ...interface{}) (int, error) {
	s := fmt.Sprint(a...)
	p.buf.WriteString(s)
	return fmt.Fprint(p.writer, s)
}

// Printf implements Printer. Output is captured and written.
func (p *TeePrinter) Printf(format string, a ...interface{}) (int, error) {
	s := fmt.Sprintf(format, a...)
	p.buf.WriteString(s)
	return fmt.Fprint(p.writer, s)
}

// Println implements Printer. Output is captured and written.
func (p *TeePrinter) Println(a ...interface{}) (int, error) {
	s := fmt.Sprintln(a...)
	p.buf.WriteString(s)
	return fmt.Fprint(p.writer, s)
}

// ForcePrint prints regardless of output capture state.
func (p *TeePrinter) ForcePrint(a ...interface{}) (int, error) {
	s := fmt.Sprint(a...)
	p.buf.WriteString(s)
	return fmt.Fprint(p.writer, s)
}

// ForcePrintf prints regardless of output capture state.
func (p *TeePrinter) ForcePrintf(format string, a ...interface{}) (int, error) {
	s := fmt.Sprintf(format, a...)
	p.buf.WriteString(s)
	return fmt.Fprint(p.writer, s)
}

// ForcePrintln prints regardless of output capture state.
func (p *TeePrinter) ForcePrintln(a ...interface{}) (int, error) {
	s := fmt.Sprintln(a...)
	p.buf.WriteString(s)
	return fmt.Fprint(p.writer, s)
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
	switch strings.ToLower(os.Getenv("CF_COLOR")) {
	case "true":
		return true
	case "false":
		return false
	}
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

// WarningColor returns the message in yellow.
func WarningColor(message string) string {
	if !colorEnabled() {
		return message
	}
	return "\033[33m" + message + "\033[0m" // yellow
}

// SuccessColor returns the message in green.
func SuccessColor(message string) string {
	if !colorEnabled() {
		return message
	}
	return "\033[32m" + message + "\033[0m" // green
}

// PromptColor returns the message in bold cyan.
func PromptColor(message string) string {
	if !colorEnabled() {
		return message
	}
	return "\033[1;36m" + message + "\033[0m" // bold cyan
}

// NewUI creates a UI instance. Matches cf/terminal.NewUI signature.
func NewUI(r io.Reader, w io.Writer, printer Printer, logger cftrace.Printer) UI {
	return &terminalUI{
		stdin:   r,
		stdout:  w,
		printer: printer,
		logger:  logger,
		scanner: bufio.NewScanner(r),
	}
}

type terminalUI struct {
	stdin   io.Reader
	stdout  io.Writer
	printer Printer
	logger  cftrace.Printer
	scanner *bufio.Scanner
}

func (ui *terminalUI) Writer() io.Writer {
	return ui.stdout
}

func (ui *terminalUI) PrintPaginator(rows []string, err error) {
	if err != nil {
		ui.Failed(err.Error())
		return
	}
	for _, row := range rows {
		ui.Say(row)
	}
}

func (ui *terminalUI) PrintCapturingNoOutput(message string, args ...interface{}) {
	if len(args) == 0 {
		fmt.Fprintf(ui.stdout, "%s", message)
	} else {
		fmt.Fprintf(ui.stdout, message, args...)
	}
}

func (ui *terminalUI) Say(message string, args ...interface{}) {
	if len(args) == 0 {
		_, _ = ui.printer.Printf("%s\n", message)
	} else {
		_, _ = ui.printer.Printf(message+"\n", args...)
	}
}

func (ui *terminalUI) Warn(message string, args ...interface{}) {
	message = fmt.Sprintf(message, args...)
	ui.Say(WarningColor(message))
}

func (ui *terminalUI) Ask(prompt string) string {
	fmt.Fprintf(ui.stdout, "\n%s%s ", prompt, PromptColor(">"))
	if ui.scanner.Scan() {
		return strings.TrimSpace(ui.scanner.Text())
	}
	return ""
}

func (ui *terminalUI) AskForPassword(prompt string) string {
	// Best-effort: falls back to visible input (no terminal raw mode)
	return ui.Ask(prompt)
}

func (ui *terminalUI) Confirm(message string) bool {
	response := ui.Ask(message)
	switch strings.ToLower(response) {
	case "y", "yes":
		return true
	}
	return false
}

func (ui *terminalUI) ConfirmDelete(modelType, modelName string) bool {
	return ui.Confirm(fmt.Sprintf("Really delete the %s %s?", modelType, EntityNameColor(modelName)))
}

func (ui *terminalUI) ConfirmDeleteWithAssociations(modelType, modelName string) bool {
	return ui.Confirm(fmt.Sprintf("Really delete the %s %s and everything associated with it?", modelType, EntityNameColor(modelName)))
}

func (ui *terminalUI) Ok() {
	ui.Say(SuccessColor("OK"))
}

func (ui *terminalUI) Failed(message string, args ...interface{}) {
	message = fmt.Sprintf(message, args...)
	ui.logger.Print("FAILED")
	ui.logger.Print(message)
	if ui.logger.WritesToConsole() {
		return
	}
	ui.Say(FailureColor("FAILED"))
	ui.Say(message)
}

func (ui *terminalUI) LoadingIndication() {
	_, _ = ui.printer.Print(".")
}

func (ui *terminalUI) Table(headers []string) *UITable {
	return &UITable{
		UI:    ui,
		Table: NewTable(headers),
	}
}

// UITable wraps a Table with a UI for printing.
type UITable struct {
	UI    UI
	Table *Table
}

func (u *UITable) Add(row ...string) {
	u.Table.Add(row...)
}

// Print formats the table and prints it via the UI.
func (u *UITable) Print() error {
	var buf strings.Builder
	err := u.Table.PrintTo(&buf)
	if err != nil {
		return err
	}
	r := buf.String()
	if len(r) > 0 && r[len(r)-1] == '\n' {
		r = r[:len(r)-1]
	}
	if len(r) > 0 {
		u.UI.Say("%s", r)
	}
	return nil
}

// Table holds tabular data for formatted output.
type Table struct {
	headers []string
	rows    [][]string
}

// NewTable creates a Table with the given headers.
func NewTable(headers []string) *Table {
	return &Table{headers: headers}
}

// Add appends a row to the table.
func (t *Table) Add(row ...string) {
	t.rows = append(t.rows, row)
}

// PrintTo writes the formatted table to w.
func (t *Table) PrintTo(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 4, 3, ' ', 0)
	if len(t.headers) > 0 {
		fmt.Fprintln(tw, strings.Join(t.headers, "\t"))
	}
	for _, row := range t.rows {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	return tw.Flush()
}
