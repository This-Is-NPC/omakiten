package httpapi

import (
	"encoding"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"time"
)

var (
	timeType            = reflect.TypeFor[time.Time]()
	jsonMarshalerType   = reflect.TypeFor[json.Marshaler]()
	jsonUnmarshalerType = reflect.TypeFor[json.Unmarshaler]()
	textMarshalerType   = reflect.TypeFor[encoding.TextMarshaler]()
)

// schemas derives JSON Schema objects from Go types by the rules
// encoding/json applies: json tags name fields, `-` hides them, embedded
// structs flatten, and a field without omitempty or a pointer is required.
// Named structs become components referenced by `$ref`.
type schemas struct {
	components map[string]any
}

func newSchemas() *schemas {
	return &schemas{components: map[string]any{}}
}

func (s *schemas) of(t reflect.Type) map[string]any {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == timeType {
		return map[string]any{"type": "string", "format": "date-time"}
	}
	if implementsAny(t, jsonMarshalerType) || implementsAny(t, jsonUnmarshalerType) {
		return map[string]any{}
	}
	if implementsAny(t, textMarshalerType) {
		return map[string]any{"type": "string"}
	}
	switch t.Kind() {
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		return map[string]any{"type": "integer", "format": "int32"}
	case reflect.Int64:
		return map[string]any{"type": "integer", "format": "int64"}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer", "minimum": 0}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return map[string]any{"type": "string", "contentEncoding": "base64"}
		}
		return map[string]any{"type": "array", "items": s.of(t.Elem())}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": s.of(t.Elem())}
	case reflect.Struct:
		if t.Name() == "" {
			return s.object(t)
		}
		name := componentName(t)
		if _, ok := s.components[name]; !ok {
			s.components[name] = map[string]any{}
			s.components[name] = s.object(t)
		}
		return map[string]any{"$ref": "#/components/schemas/" + name}
	default:
		return map[string]any{}
	}
}

func (s *schemas) object(t reflect.Type) map[string]any {
	properties := map[string]any{}
	var required []string
	s.fields(t, properties, &required)
	out := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		sort.Strings(required)
		out["required"] = required
	}
	return out
}

func (s *schemas) fields(t reflect.Type, properties map[string]any, required *[]string) {
	for i := range t.NumField() {
		field := t.Field(i)
		name, omitempty, skip := jsonField(field)
		if skip {
			continue
		}
		if embedded, ok := flattened(field, name); ok {
			s.fields(embedded, properties, required)
			continue
		}
		if !field.IsExported() {
			continue
		}
		if name == "" {
			name = field.Name
		}
		properties[name] = s.of(field.Type)
		if !omitempty && field.Type.Kind() != reflect.Pointer {
			*required = append(*required, name)
		}
	}
}

// flattened reports the struct an untagged embedded field promotes.
func flattened(field reflect.StructField, name string) (reflect.Type, bool) {
	if !field.Anonymous || name != "" {
		return nil, false
	}
	embedded := field.Type
	for embedded.Kind() == reflect.Pointer {
		embedded = embedded.Elem()
	}
	return embedded, embedded.Kind() == reflect.Struct
}

func jsonField(field reflect.StructField) (name string, omitempty, skip bool) {
	tag, ok := field.Tag.Lookup("json")
	if !ok {
		return "", false, false
	}
	if tag == "-" {
		return "", false, true
	}
	name, options, _ := strings.Cut(tag, ",")
	for option := range strings.SplitSeq(options, ",") {
		if option == "omitempty" || option == "omitzero" {
			omitempty = true
		}
	}
	return name, omitempty, false
}

func implementsAny(t, iface reflect.Type) bool {
	return t.Implements(iface) || reflect.PointerTo(t).Implements(iface)
}

// componentName qualifies a named type by its package, e.g. contract.TaskSummary.
func componentName(t reflect.Type) string {
	pkg := t.PkgPath()
	if i := strings.LastIndexByte(pkg, '/'); i >= 0 {
		pkg = pkg[i+1:]
	}
	name := strings.NewReplacer("[", "_", "]", "", "/", "_", "*", "").Replace(t.Name())
	if pkg == "" {
		return name
	}
	return pkg + "." + name
}
