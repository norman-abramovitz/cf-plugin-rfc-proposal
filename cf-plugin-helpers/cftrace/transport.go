package cftrace

import (
	"bytes"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// NewTracingTransport wraps an http.RoundTripper to log HTTP requests and
// responses when CF_TRACE is enabled. Output format follows CF CLI conventions:
// REQUEST/RESPONSE blocks with timestamps, sorted headers, and
// [PRIVATE DATA HIDDEN] for Authorization headers.
//
// If base is nil, http.DefaultTransport is used. The caller is responsible
// for configuring TLS on the base transport before wrapping — go-cfclient's
// skipTLSValidation only recognizes *http.Transport and *oauth2.Transport,
// so the tracing transport must wrap an already-configured base.
func NewTracingTransport(base http.RoundTripper, logger Printer) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &tracingTransport{base: base, logger: logger}
}

type tracingTransport struct {
	base   http.RoundTripper
	logger Printer
}

func (t *tracingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.logger == nil || (!t.logger.WritesToConsole()) {
		return t.base.RoundTrip(req)
	}

	t.logRequest(req)

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return resp, err
	}

	t.logResponse(resp)

	return resp, nil
}

func (t *tracingTransport) logRequest(req *http.Request) {
	t.logger.Printf("\nREQUEST: [%s]\n", time.Now().Format("2006-01-02T15:04:05Z07:00"))
	t.logger.Printf("%s %s HTTP/%d.%d\n", req.Method, req.URL.RequestURI(), req.ProtoMajor, req.ProtoMinor)
	t.logger.Printf("Host: %s\n", req.URL.Host)

	t.logHeaders(req.Header)

	if req.Body != nil && req.GetBody != nil {
		if body, err := req.GetBody(); err == nil {
			if data, err := io.ReadAll(body); err == nil && len(data) > 0 {
				t.logger.Printf("\n%s\n", string(data))
			}
		}
	}
	t.logger.Println()
}

func (t *tracingTransport) logResponse(resp *http.Response) {
	t.logger.Printf("RESPONSE: [%s]\n", time.Now().Format("2006-01-02T15:04:05Z07:00"))
	t.logger.Printf("HTTP/%d.%d %s\n", resp.ProtoMajor, resp.ProtoMinor, resp.Status)

	t.logHeaders(resp.Header)

	if resp.Body != nil {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err == nil {
			if len(bodyBytes) > 0 {
				t.logger.Printf("\n%s\n", string(bodyBytes))
			}
			resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}
	}
	t.logger.Println()
}

func (t *tracingTransport) logHeaders(headers http.Header) {
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		if strings.EqualFold(key, "Authorization") {
			t.logger.Printf("%s: [PRIVATE DATA HIDDEN]\n", key)
			continue
		}
		for _, val := range headers[key] {
			t.logger.Printf("%s: %s\n", key, val)
		}
	}
}
