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

// --- Curl detection tests ---

func TestCurlDetectLiteralEndpoint(t *testing.T) {
	source := `package foo

import "encoding/json"

func example(conn CLI) {
	output, _ := conn.CliCommandWithoutTerminalOutput("curl", "v2/apps")
	var apps struct{ Name string }
	json.Unmarshal([]byte(output[0]), &apps)
	_ = apps.Name
}
`
	result := scanSource(t, source)

	if len(result.CliCommandCalls) != 1 {
		t.Fatalf("expected 1 curl call, got %d", len(result.CliCommandCalls))
	}
	cc := result.CliCommandCalls[0]
	if cc.Method != "CliCommandWithoutTerminalOutput" {
		t.Errorf("expected CliCommandWithoutTerminalOutput, got %q", cc.Method)
	}
	if cc.Endpoint != "v2/apps" {
		t.Errorf("expected endpoint v2/apps, got %q", cc.Endpoint)
	}
	if cc.ResultVar != "output" {
		t.Errorf("expected result var output, got %q", cc.ResultVar)
	}
	if cc.V3Endpoint != "/v3/apps" {
		t.Errorf("expected V3 endpoint /v3/apps, got %q", cc.V3Endpoint)
	}
	if cc.TargetVar != "apps" {
		t.Errorf("expected target var apps, got %q", cc.TargetVar)
	}
	if !cc.Fields["Name"] {
		t.Error("expected Name field to be tracked")
	}
}

func TestCurlDetectVariableEndpoint(t *testing.T) {
	source := `package foo

import "encoding/json"

func example(conn CLI) {
	nextURL := "v2/spaces"
	output, _ := conn.CliCommandWithoutTerminalOutput("curl", nextURL)
	var spaces struct{ Guid string }
	json.Unmarshal([]byte(output[0]), &spaces)
	_ = spaces.Guid
}
`
	result := scanSource(t, source)

	if len(result.CliCommandCalls) != 1 {
		t.Fatalf("expected 1 curl call, got %d", len(result.CliCommandCalls))
	}
	cc := result.CliCommandCalls[0]
	if cc.Endpoint != "v2/spaces" {
		t.Errorf("expected resolved endpoint v2/spaces, got %q", cc.Endpoint)
	}
	if cc.EndpointVar != "nextURL" {
		t.Errorf("expected endpoint var nextURL, got %q", cc.EndpointVar)
	}
	if cc.V3Endpoint != "/v3/spaces" {
		t.Errorf("expected V3 endpoint /v3/spaces, got %q", cc.V3Endpoint)
	}
}

func TestCurlDetectCliCommand(t *testing.T) {
	source := `package foo

func example(conn CLI) {
	output, _ := conn.CliCommand("curl", "/v2/organizations")
	_ = output
}
`
	result := scanSource(t, source)

	if len(result.CliCommandCalls) != 1 {
		t.Fatalf("expected 1 curl call, got %d", len(result.CliCommandCalls))
	}
	cc := result.CliCommandCalls[0]
	if cc.Method != "CliCommand" {
		t.Errorf("expected CliCommand, got %q", cc.Method)
	}
	if cc.Endpoint != "/v2/organizations" {
		t.Errorf("expected /v2/organizations, got %q", cc.Endpoint)
	}
	if cc.V3Endpoint != "/v3/organizations" {
		t.Errorf("expected V3 endpoint /v3/organizations, got %q", cc.V3Endpoint)
	}
}

func TestCurlCompositeLitType(t *testing.T) {
	source := `package foo

import "encoding/json"

type AppsModel struct {
	Resources []string
	NextURL   string
}

func example(conn CLI) {
	output, _ := conn.CliCommandWithoutTerminalOutput("curl", "v2/apps")
	apps := AppsModel{}
	json.Unmarshal([]byte(output[0]), &apps)
	_ = apps.Resources
	_ = apps.NextURL
}
`
	result := scanSource(t, source)

	if len(result.CliCommandCalls) != 1 {
		t.Fatalf("expected 1 curl call, got %d", len(result.CliCommandCalls))
	}
	cc := result.CliCommandCalls[0]
	if cc.TargetType != "AppsModel" {
		t.Errorf("expected target type AppsModel, got %q", cc.TargetType)
	}
	if !cc.Fields["Resources"] {
		t.Error("expected Resources field")
	}
	if !cc.Fields["NextURL"] {
		t.Error("expected NextURL field")
	}
}

func TestCurlRangeVariableTracking(t *testing.T) {
	source := `package foo

import "encoding/json"

type AppsModel struct {
	Resources []AppModel
}
type AppModel struct {
	Entity EntityModel
}
type EntityModel struct {
	Name  string
	State string
}

func example(conn CLI) {
	output, _ := conn.CliCommandWithoutTerminalOutput("curl", "v2/apps")
	apps := AppsModel{}
	json.Unmarshal([]byte(output[0]), &apps)
	for _, app := range apps.Resources {
		_ = app.Entity.Name
		_ = app.Entity.State
	}
}
`
	result := scanSource(t, source)

	if len(result.CliCommandCalls) != 1 {
		t.Fatalf("expected 1 curl call, got %d", len(result.CliCommandCalls))
	}
	cc := result.CliCommandCalls[0]
	if !cc.Fields["Resources"] {
		t.Error("expected Resources field (from range)")
	}
	if !cc.Fields["Resources.Entity.Name"] {
		t.Error("expected Resources.Entity.Name field")
	}
	if !cc.Fields["Resources.Entity.State"] {
		t.Error("expected Resources.Entity.State field")
	}
}

func TestNonCurlCliCommandDetected(t *testing.T) {
	source := `package foo

func example(conn CLI) {
	output, _ := conn.CliCommand("push", "myapp")
	_ = output
}
`
	result := scanSource(t, source)

	if len(result.CliCommandCalls) != 1 {
		t.Fatalf("expected 1 CliCommand call, got %d", len(result.CliCommandCalls))
	}
	cc := result.CliCommandCalls[0]
	if cc.Command != "push" {
		t.Errorf("expected command push, got %q", cc.Command)
	}
	if cc.Method != "CliCommand" {
		t.Errorf("expected method CliCommand, got %q", cc.Method)
	}
	if len(cc.Args) != 1 || cc.Args[0] != "myapp" {
		t.Errorf("expected args [myapp], got %v", cc.Args)
	}
	// Non-curl calls should not have curl-specific fields
	if cc.V3Endpoint != "" {
		t.Errorf("expected no V3 endpoint for non-curl call, got %q", cc.V3Endpoint)
	}
}

func TestNonCurlCliCommandYAMLOutput(t *testing.T) {
	result := &ScanResult{
		Package: "foo",
		Methods: make(map[string]*MethodResult),
		CliCommandCalls: []*CliCommandCall{
			{
				File:    "main.go",
				Line:    10,
				Method:  "CliCommand",
				Command: "apps",
				Fields:  make(map[string]bool),
			},
		},
	}

	var buf bytes.Buffer
	if err := result.WriteYAML(&buf); err != nil {
		t.Fatal(err)
	}

	yaml := buf.String()
	for _, want := range []string{
		"cli_commands:",
		"command: apps",
		"method: CliCommand",
	} {
		if !strings.Contains(yaml, want) {
			t.Errorf("YAML missing %q:\n%s", want, yaml)
		}
	}
	// Should NOT have curl-specific fields
	if strings.Contains(yaml, "endpoint:") {
		t.Errorf("non-curl call should not have endpoint field:\n%s", yaml)
	}
}

func TestNonCurlCliCommandSummaryOutput(t *testing.T) {
	result := &ScanResult{
		Package: "foo",
		Methods: make(map[string]*MethodResult),
		CliCommandCalls: []*CliCommandCall{
			{
				File:    "main.go",
				Line:    10,
				Method:  "CliCommand",
				Command: "apps",
				Fields:  make(map[string]bool),
			},
		},
	}

	var buf bytes.Buffer
	result.WriteSummary(&buf)
	out := buf.String()

	if !strings.Contains(out, `CliCommand("apps"`) {
		t.Errorf("expected CliCommand(\"apps\") in summary:\n%s", out)
	}
	if !strings.Contains(out, "legacy") {
		t.Errorf("expected 'legacy' note in summary:\n%s", out)
	}
}

func TestMultipleCliCommands(t *testing.T) {
	source := `package foo

import "encoding/json"

func example(conn CLI) {
	output, _ := conn.CliCommand("apps")
	_ = output

	result, _ := conn.CliCommandWithoutTerminalOutput("curl", "v2/spaces")
	var spaces struct{ Name string }
	json.Unmarshal([]byte(result[0]), &spaces)
	_ = spaces.Name
}
`
	result := scanSource(t, source)

	if len(result.CliCommandCalls) != 2 {
		t.Fatalf("expected 2 CliCommand calls, got %d", len(result.CliCommandCalls))
	}

	// First: non-curl
	if result.CliCommandCalls[0].Command != "apps" {
		t.Errorf("expected first command apps, got %q", result.CliCommandCalls[0].Command)
	}

	// Second: curl with analysis
	cc := result.CliCommandCalls[1]
	if cc.Command != "curl" {
		t.Errorf("expected second command curl, got %q", cc.Command)
	}
	if cc.V3Endpoint != "/v3/spaces" {
		t.Errorf("expected V3 endpoint /v3/spaces, got %q", cc.V3Endpoint)
	}
	if cc.TargetVar != "spaces" {
		t.Errorf("expected target var spaces, got %q", cc.TargetVar)
	}
}

func TestCurlNoUnmarshalNoFields(t *testing.T) {
	// Curl call with no json.Unmarshal — should still be detected but with no target/fields
	source := `package foo

func example(conn CLI) {
	output, _ := conn.CliCommandWithoutTerminalOutput("curl", "/v2/buildpacks")
	_ = output
}
`
	result := scanSource(t, source)

	if len(result.CliCommandCalls) != 1 {
		t.Fatalf("expected 1 curl call, got %d", len(result.CliCommandCalls))
	}
	cc := result.CliCommandCalls[0]
	if cc.TargetVar != "" {
		t.Errorf("expected no target var, got %q", cc.TargetVar)
	}
	if len(cc.Fields) != 0 {
		t.Errorf("expected no fields, got %v", cc.Fields)
	}
}

func TestCurlUnknownEndpoint(t *testing.T) {
	source := `package foo

func example(conn CLI) {
	output, _ := conn.CliCommand("curl", "/v2/some_custom_endpoint")
	_ = output
}
`
	result := scanSource(t, source)

	if len(result.CliCommandCalls) != 1 {
		t.Fatalf("expected 1 curl call, got %d", len(result.CliCommandCalls))
	}
	cc := result.CliCommandCalls[0]
	if cc.V3Endpoint != "" {
		t.Errorf("expected no V3 endpoint for unknown path, got %q", cc.V3Endpoint)
	}
}

func TestCurlEndpointWithQueryParams(t *testing.T) {
	source := `package foo

func example(conn CLI) {
	output, _ := conn.CliCommand("curl", "/v2/apps?q=name:myapp")
	_ = output
}
`
	result := scanSource(t, source)

	if len(result.CliCommandCalls) != 1 {
		t.Fatalf("expected 1 curl call, got %d", len(result.CliCommandCalls))
	}
	cc := result.CliCommandCalls[0]
	if cc.V3Endpoint != "/v3/apps" {
		t.Errorf("expected /v3/apps (query stripped), got %q", cc.V3Endpoint)
	}
}

func TestCurlYAMLOutput(t *testing.T) {
	result := &ScanResult{
		Package: "foo",
		Methods: make(map[string]*MethodResult),
		CliCommandCalls: []*CliCommandCall{
			{
				File:       "main.go",
				Line:       42,
				Method:     "CliCommandWithoutTerminalOutput",
				Command:    "curl",
				Endpoint:   "v2/apps",
				ResultVar:  "output",
				TargetVar:  "apps",
				TargetType: "AppsModel",
				Fields:     map[string]bool{"Resources.Entity.Name": true, "NextURL": true},
				V3Endpoint: "/v3/apps",
				V3Notes:    "V2 entity/metadata envelope → V3 flat resources",
			},
		},
	}

	var buf bytes.Buffer
	if err := result.WriteYAML(&buf); err != nil {
		t.Fatal(err)
	}

	yaml := buf.String()
	for _, want := range []string{
		"cli_commands:",
		"command: curl",
		"file: main.go",
		"line: 42",
		"method: CliCommandWithoutTerminalOutput",
		"endpoint: v2/apps",
		"v3_endpoint: /v3/apps",
		"target_type: AppsModel",
		"fields:",
		"NextURL",
		"Resources.Entity.Name",
	} {
		if !strings.Contains(yaml, want) {
			t.Errorf("YAML missing %q:\n%s", want, yaml)
		}
	}
}

func TestCurlSummaryOutput(t *testing.T) {
	result := &ScanResult{
		Package: "foo",
		Methods: make(map[string]*MethodResult),
		CliCommandCalls: []*CliCommandCall{
			{
				File:       "main.go",
				Line:       42,
				Method:     "CliCommandWithoutTerminalOutput",
				Command:    "curl",
				Endpoint:   "v2/apps",
				TargetVar:  "apps",
				TargetType: "AppsModel",
				Fields:     map[string]bool{"Resources.Entity.Name": true},
				V3Endpoint: "/v3/apps",
				V3Notes:    "test note",
			},
		},
	}

	var buf bytes.Buffer
	result.WriteSummary(&buf)
	out := buf.String()

	for _, want := range []string{
		"Found CliCommand calls",
		"main.go:42",
		"CliCommandWithoutTerminalOutput",
		"/v3/apps",
		"test note",
		"AppsModel",
		"Resources.Entity.Name",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q:\n%s", want, out)
		}
	}
}

func TestNormalizeEndpoint(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"v2/apps", "v2/apps"},
		{"/v2/apps", "v2/apps"},
		{"/v2/apps?q=name:foo", "v2/apps"},
		{"v2/apps/some-guid/stats", "v2/apps"},
		{"/v2/service_instances", "v2/service_instances"},
	}
	for _, tt := range tests {
		got := normalizeEndpoint(tt.input)
		if got != tt.want {
			t.Errorf("normalizeEndpoint(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCurlStringsJoinPattern(t *testing.T) {
	source := `package foo

import (
	"encoding/json"
	"strings"
)

func example(conn CLI) {
	output, _ := conn.CliCommandWithoutTerminalOutput("curl", "v2/stacks")
	var stacks struct{ Name string }
	json.Unmarshal([]byte(strings.Join(output, "")), &stacks)
	_ = stacks.Name
}
`
	result := scanSource(t, source)

	if len(result.CliCommandCalls) != 1 {
		t.Fatalf("expected 1 curl call, got %d", len(result.CliCommandCalls))
	}
	cc := result.CliCommandCalls[0]
	if cc.TargetVar != "stacks" {
		t.Errorf("expected target var stacks, got %q", cc.TargetVar)
	}
	if !cc.Fields["Name"] {
		t.Error("expected Name field")
	}
}

func TestCurlMixedWithV2Methods(t *testing.T) {
	source := `package foo

import "encoding/json"

func example(conn CLI) {
	app, _ := conn.GetApp("myapp")
	_ = app.Guid

	output, _ := conn.CliCommandWithoutTerminalOutput("curl", "v2/service_instances")
	var svc struct{ Name string }
	json.Unmarshal([]byte(output[0]), &svc)
	_ = svc.Name
}
`
	result := scanSource(t, source)

	if _, ok := result.Methods["GetApp"]; !ok {
		t.Error("expected GetApp V2 method to be detected")
	}
	if len(result.CliCommandCalls) != 1 {
		t.Fatalf("expected 1 curl call, got %d", len(result.CliCommandCalls))
	}
	if result.CliCommandCalls[0].Endpoint != "v2/service_instances" {
		t.Errorf("expected v2/service_instances, got %q", result.CliCommandCalls[0].Endpoint)
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
