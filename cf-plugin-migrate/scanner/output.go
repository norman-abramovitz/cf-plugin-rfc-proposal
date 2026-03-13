package scanner

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// WriteYAML writes the scan result as a cf-plugin-migrate.yml file.
func (r *ScanResult) WriteYAML(w io.Writer) error {
	if len(r.Methods) == 0 && len(r.CliCommandCalls) == 0 && len(r.InternalImports) == 0 {
		checkWriteErr(fmt.Fprintln(w, "# No V2 domain method calls found."))
		return nil
	}

	// Build the YAML document using yaml.Node for key ordering control.
	doc := &yaml.Node{Kind: yaml.DocumentNode}
	root := &yaml.Node{Kind: yaml.MappingNode}
	doc.Content = append(doc.Content, root)

	// schema_version: 1
	addScalar(root, "schema_version", "1")

	// package
	addScalar(root, "package", r.Package)

	// methods
	root.Content = append(root.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "methods"},
	)

	methodsNode := &yaml.Node{Kind: yaml.MappingNode}
	root.Content = append(root.Content, methodsNode)

	// Output methods in a stable order matching V2 interface order.
	methodOrder := []string{
		"GetApp", "GetApps",
		"GetService", "GetServices",
		"GetOrg", "GetOrgs",
		"GetSpace", "GetSpaces",
		"GetOrgUsers", "GetSpaceUsers",
	}

	for _, method := range methodOrder {
		mr, ok := r.Methods[method]
		if !ok {
			continue
		}

		methodsNode.Content = append(methodsNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: method},
		)

		methodNode := &yaml.Node{Kind: yaml.MappingNode}
		methodsNode.Content = append(methodsNode.Content, methodNode)

		// fields — sorted, with group annotations as comments
		fields := sortedKeys(mr.Fields)
		fieldsNode := buildFieldsNode(method, fields)
		methodNode.Content = append(methodNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "fields"},
			fieldsNode,
		)

		// sub-field keys — sorted by key name
		subKeys := sortedMapKeys(mr.SubFields)
		for _, subKey := range subKeys {
			subFields := sortedKeys(mr.SubFields[subKey])
			methodNode.Content = append(methodNode.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Value: subKey},
				buildFlowSequence(subFields),
			)
		}

		// Add per-item annotation if any field group has PerItem=true
		addPerItemComment(method, mr, methodNode)
	}

	// cli_commands section — all CliCommand/CliCommandWithoutTerminalOutput calls
	if len(r.CliCommandCalls) > 0 {
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "cli_commands"},
		)

		cmdSeq := &yaml.Node{Kind: yaml.SequenceNode}
		root.Content = append(root.Content, cmdSeq)

		for _, cc := range r.CliCommandCalls {
			entry := &yaml.Node{Kind: yaml.MappingNode}
			cmdSeq.Content = append(cmdSeq.Content, entry)

			addScalar(entry, "file", cc.File)
			addScalar(entry, "line", fmt.Sprintf("%d", cc.Line))
			addScalar(entry, "method", cc.Method)
			addScalar(entry, "command", cc.Command)

			if len(cc.Args) > 0 {
				entry.Content = append(entry.Content,
					&yaml.Node{Kind: yaml.ScalarNode, Value: "args"},
					buildFlowSequence(cc.Args),
				)
			}

			// Curl-specific fields
			if cc.Command == "curl" {
				if cc.Endpoint != "" {
					addScalar(entry, "endpoint", cc.Endpoint)
				}
				if cc.EndpointVar != "" {
					addScalar(entry, "endpoint_var", cc.EndpointVar)
				}
				if cc.V3Endpoint != "" {
					addScalar(entry, "v3_endpoint", cc.V3Endpoint)
				}
				if cc.V3Notes != "" {
					addScalar(entry, "v3_notes", cc.V3Notes)
				}
				if cc.TargetType != "" {
					addScalar(entry, "target_type", cc.TargetType)
				}
				if len(cc.Fields) > 0 {
					entry.Content = append(entry.Content,
						&yaml.Node{Kind: yaml.ScalarNode, Value: "fields"},
						buildFlowSequence(sortedKeys(cc.Fields)),
					)
				}

				// Resolved endpoints from cross-function tracing
				if len(cc.ResolvedEndpoints) > 0 {
					entry.Content = append(entry.Content,
						&yaml.Node{Kind: yaml.ScalarNode, Value: "resolved_endpoints"},
					)
					epSeq := &yaml.Node{Kind: yaml.SequenceNode}
					entry.Content = append(entry.Content, epSeq)
					for _, ep := range cc.ResolvedEndpoints {
						epEntry := &yaml.Node{Kind: yaml.MappingNode}
						epSeq.Content = append(epSeq.Content, epEntry)
						addScalar(epEntry, "endpoint", ep.Endpoint)
						addScalar(epEntry, "file", ep.File)
						addScalar(epEntry, "line", fmt.Sprintf("%d", ep.Line))
						addScalar(epEntry, "caller", ep.Caller)
						if ep.V3Endpoint != "" {
							addScalar(epEntry, "v3_endpoint", ep.V3Endpoint)
						}
						if ep.V3Notes != "" {
							addScalar(epEntry, "v3_notes", ep.V3Notes)
						}
					}
				}
			}
		}
	}

	// internal_imports section — CLI internal package imports
	if len(r.InternalImports) > 0 {
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "internal_imports"},
		)

		importsSeq := &yaml.Node{Kind: yaml.SequenceNode}
		root.Content = append(root.Content, importsSeq)

		for _, ii := range r.InternalImports {
			entry := &yaml.Node{Kind: yaml.MappingNode}
			importsSeq.Content = append(importsSeq.Content, entry)

			addScalar(entry, "file", ii.File)
			addScalar(entry, "import", ii.ImportPath)
			if ii.Replacement != "" {
				addScalar(entry, "replacement", ii.Replacement)
			}
			if ii.Note != "" {
				addScalar(entry, "note", ii.Note)
			}
		}
	}

	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return err
	}
	return enc.Close()
}

// WriteSummary writes a human-readable summary of the scan to w.
func (r *ScanResult) WriteSummary(w io.Writer) {
	if len(r.Methods) == 0 && len(r.CliCommandCalls) == 0 && len(r.InternalImports) == 0 {
		checkWriteErr(fmt.Fprintln(w, "No V2 domain method calls found."))
		return
	}

	if len(r.Methods) > 0 {
		checkWriteErr(fmt.Fprintln(w, "Found V2 domain method calls:"))
	}
	checkWriteErr(fmt.Fprintln(w))

	methodOrder := []string{
		"GetApp", "GetApps",
		"GetService", "GetServices",
		"GetOrg", "GetOrgs",
		"GetSpace", "GetSpaces",
		"GetOrgUsers", "GetSpaceUsers",
	}

	for _, method := range methodOrder {
		mr, ok := r.Methods[method]
		if !ok {
			continue
		}

		fields := sortedKeys(mr.Fields)

		for _, cs := range mr.CallSites {
			if cs.Flagged {
				checkWriteErr(fmt.Fprintf(w, "  %s:%d\t%s\t⚠ %s\n",
					cs.File, cs.Line, method, cs.FlagNote))
			} else {
				checkWriteErr(fmt.Fprintf(w, "  %s:%d\t%s\t→ fields: %s\n",
					cs.File, cs.Line, method, strings.Join(fields, ", ")))
			}
		}

		// Show sub-fields
		subKeys := sortedMapKeys(mr.SubFields)
		for _, subKey := range subKeys {
			subFields := sortedKeys(mr.SubFields[subKey])
			checkWriteErr(fmt.Fprintf(w, "    %s: %s\n", subKey, strings.Join(subFields, ", ")))
		}

		// Show API call groups used
		if modelInfo, ok := V2Models[method]; ok {
			usedGroups := groupsUsed(modelInfo, mr)
			if len(usedGroups) > 0 {
				checkWriteErr(fmt.Fprintf(w, "    V3 API calls: %s\n", strings.Join(usedGroups, ", ")))
			}
		}
		checkWriteErr(fmt.Fprintln(w))
	}

	// CliCommand/CliCommandWithoutTerminalOutput calls
	if len(r.CliCommandCalls) > 0 {
		checkWriteErr(fmt.Fprintln(w, "Found CliCommand calls (legacy — not available in V3 plugin interface):"))
		checkWriteErr(fmt.Fprintln(w))

		for _, cc := range r.CliCommandCalls {
			// Build the argument display
			argParts := []string{fmt.Sprintf("%q", cc.Command)}
			for _, a := range cc.Args {
				argParts = append(argParts, fmt.Sprintf("%q", a))
			}

			checkWriteErr(fmt.Fprintf(w, "  %s:%d\t%s(%s)\n",
				cc.File, cc.Line, cc.Method, strings.Join(argParts, ", ")))

			// Curl-specific details
			if cc.Command == "curl" {
				if cc.V3Endpoint != "" {
					note := ""
					if cc.V3Notes != "" {
						note = " (" + cc.V3Notes + ")"
					}
					checkWriteErr(fmt.Fprintf(w, "    → V3 equivalent: %s%s\n", cc.V3Endpoint, note))
				}

				if cc.TargetVar != "" {
					typePart := ""
					if cc.TargetType != "" {
						typePart = " (" + cc.TargetType + ")"
					}
					checkWriteErr(fmt.Fprintf(w, "    → Unmarshalled into: %s%s\n", cc.TargetVar, typePart))
				}

				if len(cc.Fields) > 0 {
					fields := sortedKeys(cc.Fields)
					checkWriteErr(fmt.Fprintf(w, "    → Fields used: %s\n", strings.Join(fields, ", ")))
				}

				// Resolved endpoints from cross-function tracing
				if len(cc.ResolvedEndpoints) > 0 {
					checkWriteErr(fmt.Fprintf(w, "    Resolved endpoints (%d callers traced):\n", len(cc.ResolvedEndpoints)))
					for _, ep := range cc.ResolvedEndpoints {
						v3 := ""
						if ep.V3Endpoint != "" {
							v3 = " → " + ep.V3Endpoint
							if ep.V3Notes != "" {
								v3 += " (" + ep.V3Notes + ")"
							}
						}
						checkWriteErr(fmt.Fprintf(w, "      %s:%d\t%s\t%s%s\n",
							ep.File, ep.Line, ep.Caller, ep.Endpoint, v3))
					}
				}
			}
			checkWriteErr(fmt.Fprintln(w))
		}
	}

	// Internal CLI package imports
	if len(r.InternalImports) > 0 {
		checkWriteErr(fmt.Fprintln(w, "Internal CLI package imports detected (not part of public plugin contract):"))
		checkWriteErr(fmt.Fprintln(w))

		for _, ii := range r.InternalImports {
			checkWriteErr(fmt.Fprintf(w, "  %s\n", ii.File))
			checkWriteErr(fmt.Fprintf(w, "    import: %s\n", ii.ImportPath))
			if ii.Replacement != "" {
				checkWriteErr(fmt.Fprintf(w, "    → Replace with: %s\n", ii.Replacement))
			}
			if ii.Note != "" {
				checkWriteErr(fmt.Fprintf(w, "    → %s\n", ii.Note))
			}
			checkWriteErr(fmt.Fprintln(w))
		}
	}
}

// buildFieldsNode creates a YAML flow sequence for field names,
// annotated with group comments.
func buildFieldsNode(method string, fields []string) *yaml.Node {
	seq := &yaml.Node{
		Kind:  yaml.SequenceNode,
		Style: yaml.FlowStyle,
	}
	for _, f := range fields {
		seq.Content = append(seq.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: f},
		)
	}
	return seq
}

// buildFlowSequence creates a YAML flow sequence from a string slice.
func buildFlowSequence(items []string) *yaml.Node {
	seq := &yaml.Node{
		Kind:  yaml.SequenceNode,
		Style: yaml.FlowStyle,
	}
	for _, item := range items {
		seq.Content = append(seq.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: item},
		)
	}
	return seq
}

// addPerItemComment adds a comment to the method node if any detected
// field requires per-item API calls.
func addPerItemComment(method string, mr *MethodResult, methodNode *yaml.Node) {
	modelInfo, ok := V2Models[method]
	if !ok {
		return
	}

	var perItemFields []string
	for field := range mr.Fields {
		groupIdx, ok := modelInfo.FieldGroup[field]
		if !ok {
			continue
		}
		if groupIdx < len(modelInfo.Groups) && modelInfo.Groups[groupIdx].PerItem {
			perItemFields = append(perItemFields, field)
		}
	}

	if len(perItemFields) > 0 {
		sort.Strings(perItemFields)
		comment := fmt.Sprintf(" Additional calls per app: %s", strings.Join(perItemFields, ", "))
		// Attach comment to the last content node
		if len(methodNode.Content) > 0 {
			last := methodNode.Content[len(methodNode.Content)-1]
			last.FootComment = comment
		}
	}
}

// groupsUsed returns the names and API calls of groups that have at least
// one field accessed.
func groupsUsed(modelInfo *ModelInfo, mr *MethodResult) []string {
	seen := make(map[int]bool)
	for field := range mr.Fields {
		if idx, ok := modelInfo.FieldGroup[field]; ok {
			seen[idx] = true
		}
	}

	var groups []string
	for i, g := range modelInfo.Groups {
		if seen[i] {
			groups = append(groups, g.APICall)
		}
	}
	return groups
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func addScalar(mapping *yaml.Node, key, value string) {
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Value: value},
	)
}

func sortedMapKeys(m map[string]map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// onWriteErr is called when a write error occurs. In production it exits;
// tests replace this to capture the error.
var onWriteErr = func(err error) {
	fmt.Fprintf(os.Stderr, "write error: %v\n", err)
	os.Exit(1)
}

// checkWriteErr handles errors from fmt.Fprintln/fmt.Fprintf calls:
//
//	checkWriteErr(fmt.Fprintln(w, "..."))
func checkWriteErr(_ int, err error) {
	if err != nil {
		onWriteErr(err)
	}
}
