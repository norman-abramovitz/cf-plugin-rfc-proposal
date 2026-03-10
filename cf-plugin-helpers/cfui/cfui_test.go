package cfui

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"code.cloudfoundry.org/cf-plugin-helpers/cftrace"
)

func TestSay(t *testing.T) {
	var buf bytes.Buffer
	u := NewUI(nil, &buf, nil, cftrace.NewWriterPrinter(io.Discard, false))
	u.Say("hello %s", "world")
	if !strings.Contains(buf.String(), "hello world") {
		t.Errorf("expected hello world, got %q", buf.String())
	}
}

func TestSayNoArgs(t *testing.T) {
	var buf bytes.Buffer
	u := NewUI(nil, &buf, nil, cftrace.NewWriterPrinter(io.Discard, false))
	u.Say("simple message")
	if !strings.Contains(buf.String(), "simple message") {
		t.Errorf("expected simple message, got %q", buf.String())
	}
}

func TestFailed(t *testing.T) {
	UserAskedForColors = "false"
	defer func() { UserAskedForColors = "" }()

	var buf bytes.Buffer
	u := NewUI(nil, &buf, nil, cftrace.NewWriterPrinter(io.Discard, false))
	u.Failed("something went wrong")
	output := buf.String()
	if !strings.Contains(output, "FAILED") {
		t.Error("expected FAILED in output")
	}
	if !strings.Contains(output, "something went wrong") {
		t.Error("expected error message in output")
	}
}

func TestOk(t *testing.T) {
	UserAskedForColors = "false"
	defer func() { UserAskedForColors = "" }()

	var buf bytes.Buffer
	u := NewUI(nil, &buf, nil, cftrace.NewWriterPrinter(io.Discard, false))
	u.Ok()
	if !strings.Contains(buf.String(), "OK") {
		t.Errorf("expected OK, got %q", buf.String())
	}
}

func TestTable(t *testing.T) {
	var buf bytes.Buffer
	u := NewUI(nil, &buf, nil, cftrace.NewWriterPrinter(io.Discard, false))
	table := u.Table([]string{"Name", "State"})
	table.Add("app1", "started")
	table.Add("app2", "stopped")
	table.Print()

	output := buf.String()
	if !strings.Contains(output, "Name") || !strings.Contains(output, "State") {
		t.Error("expected headers in table output")
	}
	if !strings.Contains(output, "app1") || !strings.Contains(output, "started") {
		t.Error("expected row data in table output")
	}
}

func TestEntityNameColor(t *testing.T) {
	UserAskedForColors = "false"
	defer func() { UserAskedForColors = "" }()
	if got := EntityNameColor("test"); got != "test" {
		t.Errorf("expected no color when disabled, got %q", got)
	}

	UserAskedForColors = "true"
	got := EntityNameColor("test")
	if !strings.Contains(got, "test") {
		t.Errorf("expected test in colored output, got %q", got)
	}
	if !strings.Contains(got, "\033[") {
		t.Error("expected ANSI escape in colored output")
	}
}

func TestCommandColor(t *testing.T) {
	UserAskedForColors = "true"
	defer func() { UserAskedForColors = "" }()
	got := CommandColor("cf push")
	if !strings.Contains(got, "cf push") {
		t.Errorf("expected command in output, got %q", got)
	}
}

func TestFailureColor(t *testing.T) {
	UserAskedForColors = "true"
	defer func() { UserAskedForColors = "" }()
	got := FailureColor("error")
	if !strings.Contains(got, "error") {
		t.Errorf("expected error in output, got %q", got)
	}
}

func TestTeePrinter(t *testing.T) {
	var buf bytes.Buffer
	p := NewTeePrinter(&buf)
	p.Write([]byte("hello"))
	if buf.String() != "hello" {
		t.Errorf("expected writer to receive data, got %q", buf.String())
	}
	if p.String() != "hello" {
		t.Errorf("expected captured data, got %q", p.String())
	}
}

func TestInitColorSupport(t *testing.T) {
	t.Setenv("CF_COLOR", "false")
	UserAskedForColors = ""
	InitColorSupport()
	if UserAskedForColors != "false" {
		t.Errorf("expected UserAskedForColors to be 'false', got %q", UserAskedForColors)
	}
	UserAskedForColors = ""
}

func TestNewUIWithTeePrinter(t *testing.T) {
	var buf bytes.Buffer
	printer := NewTeePrinter(&buf)
	u := NewUI(nil, io.Discard, printer, cftrace.NewWriterPrinter(io.Discard, false))
	u.Say("captured")
	if !strings.Contains(printer.String(), "captured") {
		t.Error("expected output captured by tee printer")
	}
}
