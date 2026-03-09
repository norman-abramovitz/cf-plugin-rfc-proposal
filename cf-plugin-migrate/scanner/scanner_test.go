package scanner

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// requireMethod returns the MethodResult for the given method name, or fails the test.
func requireMethod(t *testing.T, result *ScanResult, method string) *MethodResult {
	t.Helper()
	mr, ok := result.Methods[method]
	if !ok || mr == nil {
		t.Fatalf("expected method %s to be detected", method)
	}
	return mr
}

// writeTestFile creates a temporary Go source file and returns its path.
func writeTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// scanSource creates a temp file from Go source, scans it, and returns the result.
func scanSource(t *testing.T, source string) *ScanResult {
	t.Helper()
	dir := t.TempDir()
	writeTestFile(t, dir, "test.go", source)
	result, err := Scan([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestIsExported(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"Guid", true},
		{"Name", true},
		{"Routes", true},
		{"run", false},
		{"cf", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isExported(tt.name); got != tt.want {
			t.Errorf("isExported(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestScanSimpleAssign(t *testing.T) {
	source := `package foo

func example(conn CLI) {
	app, _ := conn.GetApp("myapp")
	_ = app.Guid
	_ = app.Name
}
`
	result := scanSource(t, source)

	mr := requireMethod(t, result, "GetApp")
	if !mr.Fields["Guid"] {
		t.Error("expected Guid field")
	}
	if !mr.Fields["Name"] {
		t.Error("expected Name field")
	}
	if len(mr.CallSites) != 1 {
		t.Errorf("expected 1 call site, got %d", len(mr.CallSites))
	}
	if mr.CallSites[0].Flagged {
		t.Error("expected call site not to be flagged")
	}
}

func TestScanReturnStmt(t *testing.T) {
	source := `package foo

func example(conn CLI) (interface{}, error) {
	return conn.GetApps()
}
`
	result := scanSource(t, source)

	mr := requireMethod(t, result, "GetApps")
	if len(mr.CallSites) != 1 {
		t.Errorf("expected 1 call site, got %d", len(mr.CallSites))
	}
	if !mr.CallSites[0].Flagged {
		t.Error("expected call site to be flagged for return stmt")
	}
	if mr.CallSites[0].FlagNote == "" {
		t.Error("expected flag note to be set")
	}
}

func TestScanDeepSelectorChain(t *testing.T) {
	source := `package foo

func example(services S) {
	app, _ := services.CLI.GetApp("myapp")
	_ = app.Guid
}
`
	result := scanSource(t, source)

	mr := requireMethod(t, result, "GetApp")
	if !mr.Fields["Guid"] {
		t.Error("expected Guid field")
	}
}

func TestScanRangeVariable(t *testing.T) {
	source := `package foo

func example(conn CLI) {
	app, _ := conn.GetApp("myapp")
	for _, route := range app.Routes {
		_ = route.Host
		_ = route.Domain.Name
	}
}
`
	result := scanSource(t, source)

	mr := requireMethod(t, result, "GetApp")
	if !mr.Fields["Routes"] {
		t.Error("expected Routes field")
	}
	rf, ok := mr.SubFields["route_fields"]
	if !ok {
		t.Fatal("expected route_fields sub-field key")
	}
	if !rf["Host"] {
		t.Error("expected route_fields to contain Host")
	}
	if !rf["Domain.Name"] {
		t.Error("expected route_fields to contain Domain.Name")
	}
}

func TestScanIndexedAccess(t *testing.T) {
	source := `package foo

func example(conn CLI) {
	app, _ := conn.GetApp("myapp")
	_ = app.Routes[0].Host
}
`
	result := scanSource(t, source)

	mr := requireMethod(t, result, "GetApp")
	if !mr.Fields["Routes"] {
		t.Error("expected Routes field")
	}
	if rf := mr.SubFields["route_fields"]; rf == nil || !rf["Host"] {
		t.Error("expected route_fields to contain Host")
	}
}

func TestScanSubFields(t *testing.T) {
	source := `package foo

func example(conn CLI) {
	svc, _ := conn.GetService("mysvc")
	_ = svc.LastOperation.State
	_ = svc.LastOperation.Description
}
`
	result := scanSource(t, source)

	mr := requireMethod(t, result, "GetService")
	if !mr.Fields["LastOperation"] {
		t.Error("expected LastOperation field")
	}
	lof := mr.SubFields["last_operation_fields"]
	if lof == nil {
		t.Fatal("expected last_operation_fields")
	}
	if !lof["State"] {
		t.Error("expected State sub-field")
	}
	if !lof["Description"] {
		t.Error("expected Description sub-field")
	}
}

func TestScanExportedFilter(t *testing.T) {
	// Lowercase sub-field names should be filtered out (scope leak protection).
	source := `package foo

func example(conn CLI) {
	app, _ := conn.GetApp("myapp")
	for _, d := range app.Routes {
		_ = d.Host
	}
	// After range, d is back to being the receiver in real code,
	// but scanner doesn't track scope. If d.run() appeared here,
	// "run" would be lowercase and filtered.
}
`
	result := scanSource(t, source)

	mr := requireMethod(t, result, "GetApp")
	rf := mr.SubFields["route_fields"]
	if rf == nil {
		t.Fatal("expected route_fields")
	}
	if !rf["Host"] {
		t.Error("expected Host sub-field (exported)")
	}
	// Verify no lowercase entries leaked in
	for k := range rf {
		if !isExported(k) {
			t.Errorf("unexpected unexported sub-field: %q", k)
		}
	}
}

func TestScanUnknownFieldIgnored(t *testing.T) {
	source := `package foo

func example(conn CLI) {
	app, _ := conn.GetApp("myapp")
	_ = app.Guid
	_ = app.SomeFieldThatDoesNotExist
}
`
	result := scanSource(t, source)

	mr := requireMethod(t, result, "GetApp")
	if !mr.Fields["Guid"] {
		t.Error("expected Guid field")
	}
	if mr.Fields["SomeFieldThatDoesNotExist"] {
		t.Error("unknown field should not be recorded")
	}
}

func TestScanNoV2Calls(t *testing.T) {
	source := `package foo

func example() {
	x := 1 + 2
	_ = x
}
`
	result := scanSource(t, source)

	if len(result.Methods) != 0 {
		t.Errorf("expected no methods, got %d", len(result.Methods))
	}
}

func TestScanMultipleMethods(t *testing.T) {
	source := `package foo

func example(conn CLI) {
	app, _ := conn.GetApp("myapp")
	_ = app.Guid

	org, _ := conn.GetOrg("myorg")
	_ = org.Guid
	_ = org.Name
}
`
	result := scanSource(t, source)

	if len(result.Methods) != 2 {
		t.Errorf("expected 2 methods, got %d", len(result.Methods))
	}

	app := result.Methods["GetApp"]
	if app == nil || !app.Fields["Guid"] {
		t.Error("expected GetApp with Guid")
	}

	org := result.Methods["GetOrg"]
	if org == nil || !org.Fields["Guid"] || !org.Fields["Name"] {
		t.Error("expected GetOrg with Guid and Name")
	}
}

func TestScanMultipleCallSites(t *testing.T) {
	source := `package foo

func one(conn CLI) {
	app, _ := conn.GetApp("a")
	_ = app.Guid
}

func two(conn CLI) {
	app, _ := conn.GetApp("b")
	_ = app.Name
}
`
	result := scanSource(t, source)

	mr := requireMethod(t, result, "GetApp")
	if len(mr.CallSites) != 2 {
		t.Errorf("expected 2 call sites, got %d", len(mr.CallSites))
	}
	// Fields are aggregated across call sites.
	if !mr.Fields["Guid"] || !mr.Fields["Name"] {
		t.Error("expected both Guid and Name aggregated")
	}
}

func TestScanExistenceCheckEmptyFields(t *testing.T) {
	// V2 call used only for error checking — no field access.
	source := `package foo

import "fmt"

func example(conn CLI) {
	_, err := conn.GetSpace("myspace")
	if err != nil {
		fmt.Println(err)
	}
}
`
	result := scanSource(t, source)

	mr := requireMethod(t, result, "GetSpace")
	if len(mr.Fields) != 0 {
		t.Errorf("expected empty fields for existence check, got %v", mr.Fields)
	}
	if len(mr.CallSites) != 1 {
		t.Errorf("expected 1 call site, got %d", len(mr.CallSites))
	}
}

func TestScanPackageName(t *testing.T) {
	source := `package myplugin

func example(conn CLI) {
	app, _ := conn.GetApp("x")
	_ = app.Guid
}
`
	result := scanSource(t, source)

	if result.Package != "myplugin" {
		t.Errorf("expected package myplugin, got %q", result.Package)
	}
}

func TestScanSkipsTestFiles(t *testing.T) {
	dir := t.TempDir()

	// Write a regular file with a V2 call.
	writeTestFile(t, dir, "main.go", `package foo

func example(conn CLI) {
	app, _ := conn.GetApp("x")
	_ = app.Guid
}
`)

	// Write a test file with a different V2 call.
	writeTestFile(t, dir, "main_test.go", `package foo

func TestExample(conn CLI) {
	svc, _ := conn.GetService("x")
	_ = svc.Name
}
`)

	result, err := Scan([]string{dir})
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := result.Methods["GetApp"]; !ok {
		t.Error("expected GetApp from main.go")
	}
	if _, ok := result.Methods["GetService"]; ok {
		t.Error("GetService should not be detected — it's in a test file")
	}
}

func TestScanRecursivePattern(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "sub")
	if err := os.Mkdir(subdir, 0755); err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, dir, "a.go", `package foo

func a(conn CLI) {
	app, _ := conn.GetApp("x")
	_ = app.Guid
}
`)
	writeTestFile(t, subdir, "b.go", `package bar

func b(conn CLI) {
	org, _ := conn.GetOrg("x")
	_ = org.Name
}
`)

	result, err := Scan([]string{dir + "/..."})
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := result.Methods["GetApp"]; !ok {
		t.Error("expected GetApp from root dir")
	}
	if _, ok := result.Methods["GetOrg"]; !ok {
		t.Error("expected GetOrg from sub dir")
	}
}

func TestWriteYAMLEmpty(t *testing.T) {
	result := &ScanResult{Methods: make(map[string]*MethodResult)}
	var buf bytes.Buffer
	if err := result.WriteYAML(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "No V2 domain method calls found") {
		t.Errorf("expected empty message, got: %s", buf.String())
	}
}

func TestWriteYAMLContent(t *testing.T) {
	result := &ScanResult{
		Package: "myplugin",
		Methods: map[string]*MethodResult{
			"GetApp": {
				Fields: map[string]bool{"Guid": true, "Routes": true},
				SubFields: map[string]map[string]bool{
					"route_fields": {"Host": true, "Domain.Name": true},
				},
				CallSites: []CallSite{{File: "main.go", Line: 10, VarName: "app"}},
			},
		},
	}

	var buf bytes.Buffer
	if err := result.WriteYAML(&buf); err != nil {
		t.Fatal(err)
	}

	yaml := buf.String()

	for _, want := range []string{
		"schema_version: 1",
		"package: myplugin",
		"GetApp:",
		"fields:",
		"Guid",
		"Routes",
		"route_fields:",
		"Host",
		"Domain.Name",
	} {
		if !strings.Contains(yaml, want) {
			t.Errorf("YAML missing %q:\n%s", want, yaml)
		}
	}
}

func TestWriteSummaryEmpty(t *testing.T) {
	result := &ScanResult{Methods: make(map[string]*MethodResult)}
	var buf bytes.Buffer
	result.WriteSummary(&buf)
	if !strings.Contains(buf.String(), "No V2 domain method calls found") {
		t.Errorf("expected empty message, got: %s", buf.String())
	}
}

func TestWriteSummaryFlagged(t *testing.T) {
	result := &ScanResult{
		Package: "foo",
		Methods: map[string]*MethodResult{
			"GetApps": {
				Fields:    map[string]bool{},
				SubFields: map[string]map[string]bool{},
				CallSites: []CallSite{
					{File: "util.go", Line: 69, Flagged: true, FlagNote: "result returned to caller"},
				},
			},
		},
	}

	var buf bytes.Buffer
	result.WriteSummary(&buf)
	out := buf.String()

	if !strings.Contains(out, "util.go:69") {
		t.Errorf("expected call site location in summary: %s", out)
	}
	if !strings.Contains(out, "result returned to caller") {
		t.Errorf("expected flag note in summary: %s", out)
	}
}

func TestWriteYAMLPerItemComment(t *testing.T) {
	// Directly construct a result with per-item fields to test addPerItemComment.
	result := &ScanResult{
		Package: "foo",
		Methods: map[string]*MethodResult{
			"GetApps": {
				Fields:    map[string]bool{"Name": true, "TotalInstances": true, "RunningInstances": true},
				SubFields: map[string]map[string]bool{},
				CallSites: []CallSite{{File: "main.go", Line: 10, VarName: "apps"}},
			},
		},
	}

	var buf bytes.Buffer
	if err := result.WriteYAML(&buf); err != nil {
		t.Fatal(err)
	}
	yaml := buf.String()
	if !strings.Contains(yaml, "Additional calls per app") {
		t.Errorf("expected per-item comment in YAML:\n%s", yaml)
	}
	if !strings.Contains(yaml, "TotalInstances") {
		t.Errorf("expected TotalInstances in per-item comment:\n%s", yaml)
	}
}

func TestWriteSummaryWithFields(t *testing.T) {
	result := &ScanResult{
		Package: "foo",
		Methods: map[string]*MethodResult{
			"GetApp": {
				Fields: map[string]bool{"Guid": true, "Name": true},
				SubFields: map[string]map[string]bool{
					"route_fields": {"Host": true},
				},
				CallSites: []CallSite{
					{File: "main.go", Line: 10, VarName: "app"},
				},
			},
		},
	}

	var buf bytes.Buffer
	result.WriteSummary(&buf)
	out := buf.String()

	if !strings.Contains(out, "main.go:10") {
		t.Errorf("expected call site in summary: %s", out)
	}
	if !strings.Contains(out, "Guid") {
		t.Errorf("expected Guid in fields: %s", out)
	}
	if !strings.Contains(out, "route_fields") || !strings.Contains(out, "Host") {
		t.Errorf("expected sub-fields in summary: %s", out)
	}
	if !strings.Contains(out, "V3 API calls") {
		t.Errorf("expected V3 API calls annotation: %s", out)
	}
}

func TestWriteSummaryGroupsUsed(t *testing.T) {
	// GetService with LastOperation should show the API call.
	result := &ScanResult{
		Package: "foo",
		Methods: map[string]*MethodResult{
			"GetService": {
				Fields:    map[string]bool{"LastOperation": true, "Name": true},
				SubFields: map[string]map[string]bool{},
				CallSites: []CallSite{
					{File: "svc.go", Line: 5, VarName: "svc"},
				},
			},
		},
	}

	var buf bytes.Buffer
	result.WriteSummary(&buf)
	out := buf.String()

	if !strings.Contains(out, "ServiceInstances.Single") {
		t.Errorf("expected API call in summary: %s", out)
	}
}

func TestScanGetAppsRangeTracking(t *testing.T) {
	// Verify range over a direct result variable (not a field).
	// GetApps returns a slice — ranging directly over it is different
	// from ranging over app.Routes (a field of the result).
	source := `package foo

func example(conn CLI) {
	apps, _ := conn.GetApps()
	for _, a := range apps {
		_ = a.Name
		_ = a.Guid
	}
}
`
	result := scanSource(t, source)

	// apps is the result var, not a field — so ranging over it
	// doesn't match the "range resultVar.Field" pattern.
	// The scanner should still detect GetApps with a call site.
	mr := requireMethod(t, result, "GetApps")
	if len(mr.CallSites) != 1 {
		t.Errorf("expected 1 call site, got %d", len(mr.CallSites))
	}
}

// errWriter is an io.Writer that always returns an error.
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) {
	return 0, errors.New("simulated write failure")
}

func TestCheckWriteErr(t *testing.T) {
	// Replace onWriteErr so we capture instead of os.Exit.
	var captured error
	orig := onWriteErr
	onWriteErr = func(err error) { captured = err }
	defer func() { onWriteErr = orig }()

	// No error — should not trigger.
	checkWriteErr(5, nil)
	if captured != nil {
		t.Error("expected no error for nil")
	}

	// With error — should capture it.
	dummy := errors.New("test write error")
	checkWriteErr(0, dummy)
	if captured == nil {
		t.Fatal("expected error to be captured")
	}
	if captured.Error() != "test write error" {
		t.Errorf("expected 'test write error', got %q", captured.Error())
	}
}

func TestWriteSummaryWriteError(t *testing.T) {
	var captured error
	orig := onWriteErr
	onWriteErr = func(err error) { captured = err }
	defer func() { onWriteErr = orig }()

	result := &ScanResult{
		Package: "foo",
		Methods: map[string]*MethodResult{
			"GetApp": {
				Fields:    map[string]bool{"Guid": true},
				SubFields: map[string]map[string]bool{},
				CallSites: []CallSite{{File: "x.go", Line: 1}},
			},
		},
	}

	result.WriteSummary(failWriter{})
	if captured == nil {
		t.Fatal("expected write error to be captured")
	}
	if !strings.Contains(captured.Error(), "simulated write failure") {
		t.Errorf("unexpected error: %v", captured)
	}
}
