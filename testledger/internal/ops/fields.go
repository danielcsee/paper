// Reading an input struct's shape once, so both transports describe the same
// arguments. A field's tags carry everything each surface needs: the wire name,
// the prose description, whether it is required, and where the CLI accepts it.
package ops

import (
	"reflect"
	"strconv"
	"strings"
)

// Field is one input argument, as both transports see it.
type Field struct {
	// Name is the wire name: the JSON property and the CLI flag.
	Name string
	// Desc is one sentence, shown in MCP schemas and CLI help alike.
	Desc string
	// Required fields appear in the schema's required list, and the CLI
	// refuses to run without them.
	Required bool
	// Position, when non-zero, is the 1-based index at which the CLI also
	// accepts this field as a bare argument. Flags stay available either way.
	Position int
	// Enum constrains a string field to a fixed set.
	Enum []string
	// Kind is the underlying Go kind: string, bool, int, or slice.
	Kind reflect.Kind
	// Elem is the slice element kind for Kind == reflect.Slice.
	Elem reflect.Kind
	// Structured marks a field the CLI cannot express as a flag -- a slice of
	// structs, say. The CLI reads it from a JSON file instead.
	Structured bool

	// index is the reflect field path, which has more than one element when the
	// field comes from an embedded struct such as Page or Confirm.
	index []int
}

// Fields describes an operation's input struct. Embedded structs are flattened,
// so the shared Page and Confirm fields appear as ordinary arguments on both
// surfaces rather than as a nested object.
func Fields(input any) []Field {
	return fieldsOf(reflect.ValueOf(input).Elem().Type(), nil)
}

func fieldsOf(value reflect.Type, prefix []int) []Field {
	out := []Field{}
	for i := 0; i < value.NumField(); i++ {
		structField := value.Field(i)
		path := append(append([]int{}, prefix...), i)
		if structField.Anonymous && structField.Type.Kind() == reflect.Struct {
			out = append(out, fieldsOf(structField.Type, path)...)
			continue
		}
		name := strings.Split(structField.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		field := Field{
			Name:     name,
			Desc:     structField.Tag.Get("desc"),
			Required: structField.Tag.Get("req") == "true",
			Kind:     structField.Type.Kind(),
			index:    path,
		}
		if position := structField.Tag.Get("pos"); position != "" {
			field.Position, _ = strconv.Atoi(position)
		}
		if enum := structField.Tag.Get("enum"); enum != "" {
			field.Enum = strings.Split(enum, ",")
		}
		if field.Kind == reflect.Slice {
			field.Elem = structField.Type.Elem().Kind()
			field.Structured = field.Elem != reflect.String
		}
		if field.Kind == reflect.Struct || field.Kind == reflect.Map || field.Kind == reflect.Ptr {
			field.Structured = true
		}
		out = append(out, field)
	}
	return out
}

// set writes a parsed value into the input struct.
func (f Field) set(input any, value any) {
	f.get(input).Set(reflect.ValueOf(value))
}

// get reads a field back out, used when validating required arguments.
func (f Field) get(input any) reflect.Value {
	return reflect.ValueOf(input).Elem().FieldByIndex(f.index)
}

// isZero reports whether the caller left a field unset.
func (f Field) isZero(input any) bool {
	return f.get(input).IsZero()
}

// flag is the hyphenated spelling used in help text and error messages. Both
// spellings are accepted when parsing.
func (f Field) flag() string {
	return strings.ReplaceAll(f.Name, "_", "-")
}
