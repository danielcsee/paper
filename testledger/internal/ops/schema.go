// JSON Schema generation for the MCP transport, derived from the same field
// descriptions the CLI uses for its flags.
package ops

import "reflect"

// InputSchema is the MCP inputSchema for one operation.
//
// Structured fields -- a slice of proposal cases, say -- are described from the
// Go type they decode into, so the schema and the decoder cannot disagree.
func InputSchema(op *Op) map[string]any {
	properties := map[string]any{}
	required := []string{}
	for _, field := range Fields(op.New()) {
		properties[field.Name] = fieldSchema(op, field)
		if field.Required {
			required = append(required, field.Name)
		}
	}
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func fieldSchema(op *Op, field Field) map[string]any {
	if len(field.Enum) > 0 {
		return map[string]any{"type": "string", "description": field.Desc, "enum": field.Enum}
	}
	switch field.Kind {
	case reflect.String:
		return map[string]any{"type": "string", "description": field.Desc}
	case reflect.Bool:
		return map[string]any{"type": "boolean", "description": field.Desc}
	case reflect.Int:
		return map[string]any{"type": "integer", "description": field.Desc, "minimum": 0}
	case reflect.Slice:
		return map[string]any{"type": "array", "description": field.Desc, "items": elementSchema(op, field)}
	}
	return map[string]any{"description": field.Desc}
}

func elementSchema(op *Op, field Field) map[string]any {
	if field.Elem == reflect.String {
		return map[string]any{"type": "string"}
	}
	elem := field.get(op.New()).Type().Elem()
	return structSchema(elem)
}

// structSchema describes a nested struct from its own json and desc tags.
func structSchema(t reflect.Type) map[string]any {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return map[string]any{"type": "object", "additionalProperties": true}
	}
	properties := map[string]any{}
	required := []string{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := tagName(f.Tag.Get("json"))
		if name == "" || name == "-" {
			continue
		}
		entry := map[string]any{"description": f.Tag.Get("desc")}
		switch f.Type.Kind() {
		case reflect.String:
			entry["type"] = "string"
		case reflect.Bool:
			entry["type"] = "boolean"
		case reflect.Int, reflect.Int64:
			entry["type"] = "integer"
		case reflect.Slice:
			entry["type"] = "array"
			if f.Type.Elem().Kind() == reflect.String {
				entry["items"] = map[string]any{"type": "string"}
			} else {
				entry["items"] = structSchema(f.Type.Elem())
			}
		default:
			entry["type"] = "object"
		}
		properties[name] = entry
		if f.Tag.Get("req") == "true" {
			required = append(required, name)
		}
	}
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func tagName(tag string) string {
	for i := 0; i < len(tag); i++ {
		if tag[i] == ',' {
			return tag[:i]
		}
	}
	return tag
}

// Annotations are the MCP behavioural hints for one operation.
func Annotations(op *Op) map[string]any {
	return map[string]any{"readOnlyHint": op.ReadOnly, "destructiveHint": false, "openWorldHint": false}
}
