package cftrace

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewWriterPrinter(t *testing.T) {
	var buf bytes.Buffer
	p := NewWriterPrinter(&buf, true)

	p.Println("hello")
	if !strings.Contains(buf.String(), "hello") {
		t.Errorf("expected output to contain hello, got %q", buf.String())
	}
	if !p.WritesToConsole() {
		t.Error("expected WritesToConsole to be true")
	}
}

func TestNewWriterPrinterNotConsole(t *testing.T) {
	var buf bytes.Buffer
	p := NewWriterPrinter(&buf, false)
	if p.WritesToConsole() {
		t.Error("expected WritesToConsole to be false")
	}
}

func TestNewLoggerVerbose(t *testing.T) {
	var buf bytes.Buffer
	p := NewLogger(&buf, true)
	p.Printf("test %s", "message")
	if !strings.Contains(buf.String(), "test message") {
		t.Errorf("expected output, got %q", buf.String())
	}
}

func TestNewLoggerCFTraceTrue(t *testing.T) {
	t.Setenv("CF_TRACE", "true")
	var buf bytes.Buffer
	p := NewLogger(&buf, false)
	if !p.WritesToConsole() {
		t.Error("expected CF_TRACE=true to enable console output")
	}
}

func TestNewLoggerSilent(t *testing.T) {
	t.Setenv("CF_TRACE", "")
	var buf bytes.Buffer
	p := NewLogger(&buf, false)
	p.Println("should not appear")
	if buf.Len() > 0 {
		t.Errorf("expected no output, got %q", buf.String())
	}
}

func TestNullPrinter(t *testing.T) {
	p := &nullPrinter{}
	p.Print("test")
	p.Printf("%s", "test")
	p.Println("test")
	if p.WritesToConsole() {
		t.Error("null printer should not write to console")
	}
}

func TestTracingTransportLogsRequest(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWriterPrinter(&buf, true)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	transport := NewTracingTransport(http.DefaultTransport, logger)
	client := &http.Client{Transport: transport}

	resp, err := client.Get(server.URL + "/v3/apps")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	output := buf.String()
	if !strings.Contains(output, "REQUEST:") {
		t.Error("expected REQUEST block in trace output")
	}
	if !strings.Contains(output, "RESPONSE:") {
		t.Error("expected RESPONSE block in trace output")
	}
	if !strings.Contains(output, "GET /v3/apps") {
		t.Errorf("expected GET /v3/apps in output, got:\n%s", output)
	}
}

func TestTracingTransportHidesAuth(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWriterPrinter(&buf, true)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer server.Close()

	transport := NewTracingTransport(http.DefaultTransport, logger)
	client := &http.Client{Transport: transport}

	req, _ := http.NewRequest("GET", server.URL+"/v3/apps", nil)
	req.Header.Set("Authorization", "bearer secret-token")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	output := buf.String()
	if strings.Contains(output, "secret-token") {
		t.Error("authorization token should be hidden in trace output")
	}
	if !strings.Contains(output, "[PRIVATE DATA HIDDEN]") {
		t.Error("expected [PRIVATE DATA HIDDEN] for auth header")
	}
}

func TestTracingTransportSilentWhenDisabled(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWriterPrinter(&buf, false) // not console

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer server.Close()

	transport := NewTracingTransport(http.DefaultTransport, logger)
	client := &http.Client{Transport: transport}

	resp, err := client.Get(server.URL + "/v3/apps")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if buf.Len() > 0 {
		t.Errorf("expected no trace output when disabled, got:\n%s", buf.String())
	}
}

func TestTracingTransportNilBase(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWriterPrinter(&buf, true)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer server.Close()

	// nil base should use DefaultTransport
	transport := NewTracingTransport(nil, logger)
	client := &http.Client{Transport: transport}

	resp, err := client.Get(server.URL + "/v3/apps")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if !strings.Contains(buf.String(), "REQUEST:") {
		t.Error("expected trace output with nil base transport")
	}
}
