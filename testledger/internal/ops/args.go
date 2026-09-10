// Parsing CLI arguments into the same input struct the MCP transport decodes
// JSON into.
//
// This does not use the standard flag package. `flag` stops parsing at the
// first non-flag argument, which silently ignored anything written after a
// positional -- `decide APPROVED_ID --json` parsed as if --json were absent.
// Knowing each field's type up front makes a correct pass simple: a token is a
// flag if it starts with '-', it consumes a value unless it is boolean, and
// everything else is positional wherever it appears.
package ops

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
)

// InputFlag is the universal escape hatch: any operation accepts its whole
// input as a JSON document, which is how the CLI expresses arguments too
// nested to be flags.
const InputFlag = "input"

// ParseArgs populates a fresh input struct for op from CLI arguments.
func ParseArgs(op *Op, args []string) (any, error) {
	input := op.New()
	fields := Fields(input)
	byName := map[string]Field{}
	for _, field := range fields {
		byName[field.Name] = field
		byName[strings.ReplaceAll(field.Name, "_", "-")] = field
	}

	positionals := []string{}
	for i := 0; i < len(args); i++ {
		token := args[i]
		if token == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(token, "-") || token == "-" {
			positionals = append(positionals, token)
			continue
		}
		name := strings.TrimLeft(token, "-")
		value := ""
		hasValue := false
		if equals := strings.Index(name, "="); equals >= 0 {
			name, value, hasValue = name[:equals], name[equals+1:], true
		}
		if name == InputFlag {
			if !hasValue {
				if i+1 >= len(args) {
					return nil, fmt.Errorf("--%s requires a file path", InputFlag)
				}
				i++
				value = args[i]
			}
			if err := loadInputFile(value, input); err != nil {
				return nil, err
			}
			continue
		}
		field, known := byName[name]
		if !known {
			return nil, fmt.Errorf("unknown flag --%s for %s", name, op.Verb)
		}
		if field.Kind == reflect.Bool && !hasValue {
			field.set(input, true)
			continue
		}
		if !hasValue {
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--%s requires a value", name)
			}
			i++
			value = args[i]
		}
		if err := assign(input, field, value); err != nil {
			return nil, err
		}
	}

	if err := applyPositionals(input, fields, positionals); err != nil {
		return nil, err
	}
	return input, validate(op, input, fields)
}

// applyPositionals fills fields tagged with a position, then hands any surplus
// to the highest-positioned string slice (test selectors, symbol keys).
func applyPositionals(input any, fields []Field, positionals []string) error {
	remaining := positionals
	for index := 1; len(remaining) > 0; index++ {
		field, found := fieldAt(fields, index)
		if !found {
			break
		}
		if field.Kind == reflect.Slice && field.Elem == reflect.String {
			if field.isZero(input) {
				field.set(input, append([]string{}, remaining...))
			}
			return nil
		}
		if field.isZero(input) {
			if err := assign(input, field, remaining[0]); err != nil {
				return err
			}
		}
		remaining = remaining[1:]
	}
	if len(remaining) > 0 {
		return fmt.Errorf("unexpected argument %q", remaining[0])
	}
	return nil
}

func fieldAt(fields []Field, position int) (Field, bool) {
	for _, field := range fields {
		if field.Position == position {
			return field, true
		}
	}
	return Field{}, false
}

func assign(input any, field Field, raw string) error {
	if len(field.Enum) > 0 && !contains(field.Enum, raw) {
		return fmt.Errorf("--%s must be one of %s", field.flag(), strings.Join(field.Enum, ", "))
	}
	switch {
	case field.Structured:
		// Too nested for a flag: the value names a JSON file.
		contents, err := os.ReadFile(raw)
		if err != nil {
			return err
		}
		target := reflect.New(field.get(input).Type())
		if err := json.Unmarshal(contents, target.Interface()); err != nil {
			return fmt.Errorf("%s: %w", raw, err)
		}
		field.get(input).Set(target.Elem())
	case field.Kind == reflect.String:
		field.set(input, raw)
	case field.Kind == reflect.Bool:
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return fmt.Errorf("--%s expects true or false", field.flag())
		}
		field.set(input, parsed)
	case field.Kind == reflect.Int:
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf("--%s expects a number", field.flag())
		}
		field.set(input, parsed)
	case field.Kind == reflect.Slice && field.Elem == reflect.String:
		existing := field.get(input).Interface().([]string)
		field.set(input, append(existing, raw))
	default:
		return fmt.Errorf("--%s cannot be set from the command line", field.flag())
	}
	return nil
}

// loadInputFile decodes a whole JSON document over the input struct. Flags
// given alongside it still win, because they are applied in argument order.
func loadInputFile(path string, input any) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(contents, input); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func validate(op *Op, input any, fields []Field) error {
	for _, field := range fields {
		if field.Required && field.isZero(input) {
			return fmt.Errorf("%s requires --%s", op.Verb, field.flag())
		}
	}
	return nil
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

// Usage renders one operation's arguments for CLI help.
func Usage(op *Op) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %-12s %s\n", op.Verb, op.Summary)
	for _, field := range Fields(op.New()) {
		marker := " "
		if field.Required {
			marker = "*"
		}
		hint := kindHint(field)
		fmt.Fprintf(&b, "      %s --%-16s %-9s %s\n", marker, field.flag(), hint, field.Desc)
	}
	return b.String()
}

func kindHint(field Field) string {
	switch {
	case field.Structured:
		return "<file>"
	case len(field.Enum) > 0:
		return "<" + strings.Join(field.Enum, "|") + ">"
	case field.Kind == reflect.Bool:
		return ""
	case field.Kind == reflect.Int:
		return "<n>"
	case field.Kind == reflect.Slice:
		return "<value>…"
	}
	return "<value>"
}
