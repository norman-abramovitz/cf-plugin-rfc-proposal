package generator

import (
	"fmt"
	"os"
	"sort"

	"cf-plugin-migrate/scanner"

	"gopkg.in/yaml.v3"
)

// GenerateConfig represents the parsed cf-plugin-migrate.yml file.
type GenerateConfig struct {
	SchemaVersion string                   `yaml:"schema_version"`
	Package       string                   `yaml:"package"`
	Methods       map[string]*MethodConfig `yaml:"methods"`
}

// MethodConfig represents a single method's configuration from the YAML.
type MethodConfig struct {
	Fields    []string                     `yaml:"fields"`
	SubFields map[string][]string          `yaml:"-"` // populated from dynamic keys like route_fields
	Extra     map[string]yaml.Node         `yaml:"-"` // captures unknown keys for sub-field parsing
}

// UnmarshalYAML implements custom unmarshalling to handle dynamic sub-field keys
// (e.g., route_fields, service_fields) alongside the static fields key.
func (mc *MethodConfig) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("expected mapping node, got %d", value.Kind)
	}

	mc.SubFields = make(map[string][]string)

	for i := 0; i < len(value.Content)-1; i += 2 {
		key := value.Content[i].Value
		val := value.Content[i+1]

		if key == "fields" {
			var fields []string
			if err := val.Decode(&fields); err != nil {
				return fmt.Errorf("decoding fields: %w", err)
			}
			mc.Fields = fields
		} else {
			// Dynamic sub-field key (e.g., route_fields, service_plan_fields)
			var subFields []string
			if err := val.Decode(&subFields); err != nil {
				return fmt.Errorf("decoding %s: %w", key, err)
			}
			mc.SubFields[key] = subFields
		}
	}

	return nil
}

// LoadConfig reads and parses a cf-plugin-migrate.yml file.
func LoadConfig(path string) (*GenerateConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	return ParseConfig(data)
}

// ParseConfig parses YAML data into a GenerateConfig and validates it.
func ParseConfig(data []byte) (*GenerateConfig, error) {
	var config GenerateConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}

	if err := validateConfig(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// validateConfig checks the parsed config against V2Models for correctness.
func validateConfig(config *GenerateConfig) error {
	if config.SchemaVersion == "" {
		return fmt.Errorf("missing required field: schema_version")
	}
	if config.SchemaVersion != "1" {
		return fmt.Errorf("unsupported schema_version: %s (expected \"1\")", config.SchemaVersion)
	}
	if config.Package == "" {
		return fmt.Errorf("missing required field: package")
	}

	for method, mc := range config.Methods {
		modelInfo, ok := scanner.V2Models[method]
		if !ok {
			return fmt.Errorf("unknown method: %s (valid: %s)", method, validMethods())
		}

		// Validate fields against the model's known fields.
		for _, field := range mc.Fields {
			if _, ok := modelInfo.FieldGroup[field]; !ok {
				return fmt.Errorf("method %s: unknown field %q (valid: %s)",
					method, field, validFields(modelInfo))
			}
		}

		// Validate sub-field keys against the model's known sub-field keys.
		for subKey := range mc.SubFields {
			found := false
			for _, knownKey := range modelInfo.SubFieldKeys {
				if subKey == knownKey {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("method %s: unknown sub-field key %q (valid: %s)",
					method, subKey, validSubFieldKeys(modelInfo))
			}

			// Verify the parent field for this sub-field key is in the fields list.
			parentField := subFieldParent(modelInfo, subKey)
			if parentField != "" && !containsField(mc.Fields, parentField) {
				return fmt.Errorf("method %s: sub-field key %q requires field %q in fields list",
					method, subKey, parentField)
			}
		}
	}

	return nil
}

// subFieldParent returns the parent field name for a given sub-field key.
func subFieldParent(modelInfo *scanner.ModelInfo, subKey string) string {
	for field, key := range modelInfo.SubFieldKeys {
		if key == subKey {
			return field
		}
	}
	return ""
}

func containsField(fields []string, target string) bool {
	for _, f := range fields {
		if f == target {
			return true
		}
	}
	return false
}

func validMethods() string {
	methods := make([]string, 0, len(scanner.V2Methods))
	for m := range scanner.V2Methods {
		methods = append(methods, m)
	}
	sort.Strings(methods)
	return fmt.Sprintf("%v", methods)
}

func validFields(modelInfo *scanner.ModelInfo) string {
	fields := make([]string, 0, len(modelInfo.FieldGroup))
	for f := range modelInfo.FieldGroup {
		fields = append(fields, f)
	}
	sort.Strings(fields)
	return fmt.Sprintf("%v", fields)
}

func validSubFieldKeys(modelInfo *scanner.ModelInfo) string {
	keys := make([]string, 0, len(modelInfo.SubFieldKeys))
	for _, k := range modelInfo.SubFieldKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return fmt.Sprintf("%v", keys)
}
