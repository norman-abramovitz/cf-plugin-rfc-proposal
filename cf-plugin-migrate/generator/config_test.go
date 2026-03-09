package generator

import (
	"strings"
	"testing"
)

func TestParseConfig_Valid(t *testing.T) {
	yaml := `
schema_version: "1"
package: main
methods:
  GetApp:
    fields: [Name, Guid, Routes]
    route_fields: [Host, Domain.Name]
  GetApps:
    fields: [Name, Guid]
`
	config, err := ParseConfig([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}

	if config.SchemaVersion != "1" {
		t.Errorf("schema_version = %q, want %q", config.SchemaVersion, "1")
	}
	if config.Package != "main" {
		t.Errorf("package = %q, want %q", config.Package, "main")
	}
	if len(config.Methods) != 2 {
		t.Fatalf("len(methods) = %d, want 2", len(config.Methods))
	}

	app := config.Methods["GetApp"]
	if app == nil {
		t.Fatal("GetApp not found")
	}
	if len(app.Fields) != 3 {
		t.Errorf("GetApp fields = %v, want 3 fields", app.Fields)
	}
	if len(app.SubFields["route_fields"]) != 2 {
		t.Errorf("GetApp route_fields = %v, want 2 sub-fields", app.SubFields["route_fields"])
	}

	apps := config.Methods["GetApps"]
	if apps == nil {
		t.Fatal("GetApps not found")
	}
	if len(apps.Fields) != 2 {
		t.Errorf("GetApps fields = %v, want 2 fields", apps.Fields)
	}
}

func TestParseConfig_EmptyMethods(t *testing.T) {
	yaml := `
schema_version: "1"
package: main
methods: {}
`
	config, err := ParseConfig([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Methods) != 0 {
		t.Errorf("len(methods) = %d, want 0", len(config.Methods))
	}
}

func TestParseConfig_MissingSchemaVersion(t *testing.T) {
	yaml := `
package: main
methods: {}
`
	_, err := ParseConfig([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for missing schema_version")
	}
	if !strings.Contains(err.Error(), "schema_version") {
		t.Errorf("error = %q, want mention of schema_version", err.Error())
	}
}

func TestParseConfig_UnsupportedSchemaVersion(t *testing.T) {
	yaml := `
schema_version: "2"
package: main
methods: {}
`
	_, err := ParseConfig([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for unsupported schema_version")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("error = %q, want mention of unsupported", err.Error())
	}
}

func TestParseConfig_MissingPackage(t *testing.T) {
	yaml := `
schema_version: "1"
methods: {}
`
	_, err := ParseConfig([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for missing package")
	}
	if !strings.Contains(err.Error(), "package") {
		t.Errorf("error = %q, want mention of package", err.Error())
	}
}

func TestParseConfig_UnknownMethod(t *testing.T) {
	yaml := `
schema_version: "1"
package: main
methods:
  GetFoo:
    fields: [Name]
`
	_, err := ParseConfig([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for unknown method")
	}
	if !strings.Contains(err.Error(), "GetFoo") {
		t.Errorf("error = %q, want mention of GetFoo", err.Error())
	}
}

func TestParseConfig_UnknownField(t *testing.T) {
	yaml := `
schema_version: "1"
package: main
methods:
  GetApp:
    fields: [Name, Bogus]
`
	_, err := ParseConfig([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
	if !strings.Contains(err.Error(), "Bogus") {
		t.Errorf("error = %q, want mention of Bogus", err.Error())
	}
}

func TestParseConfig_UnknownSubFieldKey(t *testing.T) {
	yaml := `
schema_version: "1"
package: main
methods:
  GetApp:
    fields: [Name, Routes]
    bogus_fields: [Foo]
`
	_, err := ParseConfig([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for unknown sub-field key")
	}
	if !strings.Contains(err.Error(), "bogus_fields") {
		t.Errorf("error = %q, want mention of bogus_fields", err.Error())
	}
}

func TestParseConfig_SubFieldWithoutParent(t *testing.T) {
	yaml := `
schema_version: "1"
package: main
methods:
  GetApp:
    fields: [Name, Guid]
    route_fields: [Host]
`
	_, err := ParseConfig([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for sub-field without parent")
	}
	if !strings.Contains(err.Error(), "Routes") {
		t.Errorf("error = %q, want mention of Routes", err.Error())
	}
}

func TestParseConfig_AllTenMethods(t *testing.T) {
	yaml := `
schema_version: "1"
package: main
methods:
  GetApp:
    fields: [Guid]
  GetApps:
    fields: [Guid]
  GetService:
    fields: [Guid]
  GetServices:
    fields: [Guid]
  GetOrg:
    fields: [Guid]
  GetOrgs:
    fields: [Guid]
  GetSpace:
    fields: [Guid]
  GetSpaces:
    fields: [Guid]
  GetOrgUsers:
    fields: [Guid]
  GetSpaceUsers:
    fields: [Guid]
`
	config, err := ParseConfig([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Methods) != 10 {
		t.Errorf("len(methods) = %d, want 10", len(config.Methods))
	}
}
