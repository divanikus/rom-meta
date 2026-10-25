// Package httpdocs serves offline OpenAPI definitions and vendored Swagger UI.
package httpdocs

import (
	"encoding/json"
	"reflect"
	"strings"
	"time"
)

type Schema = map[string]any

// Schemas derives wire shapes from the same DTOs encoded by handlers. Endpoint
// semantics and constraints live beside their handlers, not in duplicate structs.
type Schemas struct {
	Types  map[string]any
	owners map[string]reflect.Type
}

func NewSchemas() *Schemas {
	return &Schemas{Types: map[string]any{}, owners: map[string]reflect.Type{}}
}
func (s *Schemas) Ref(value any) Schema { return s.schema(reflect.TypeOf(value)) }
func (s *Schemas) schema(t reflect.Type) Schema {
	if t == nil {
		return Schema{}
	}
	if t.Kind() == reflect.Pointer {
		return s.schema(t.Elem())
	}
	if t == reflect.TypeOf(json.RawMessage{}) {
		return Schema{}
	}
	if t == reflect.TypeOf(time.Time{}) {
		return Schema{"type": "string", "format": "date-time"}
	}
	switch t.Kind() {
	case reflect.Struct:
		pkg := t.PkgPath()
		if i := strings.LastIndex(pkg, "/"); i >= 0 {
			pkg = pkg[i+1:]
		}
		name := t.Name()
		if name != "" {
			name = strings.ToUpper(name[:1]) + name[1:]
		}
		if pkg == "exporter" && name == "Selection" {
			name = "SelectionPolicy"
		}
		if owner, ok := s.owners[name]; ok && owner != t {
			name = pkg + "_" + name
		}
		s.owners[name] = t
		if _, ok := s.Types[name]; !ok {
			s.Types[name] = Schema{}
			s.Types[name] = s.object(t)
		}
		return Schema{"$ref": "#/components/schemas/" + name}
	case reflect.Slice, reflect.Array:
		return Schema{"type": "array", "items": s.schema(t.Elem()), "nullable": t.Kind() == reflect.Slice}
	case reflect.Map:
		return Schema{"type": "object", "additionalProperties": s.schema(t.Elem()), "nullable": true}
	case reflect.Bool:
		return Schema{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return Schema{"type": "integer", "format": "int64"}
	case reflect.Float32, reflect.Float64:
		return Schema{"type": "number", "format": "double"}
	case reflect.String:
		return Schema{"type": "string"}
	default:
		return Schema{}
	}
}
func (s *Schemas) object(t reflect.Type) Schema {
	props := Schema{}
	required := []string{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		tag := f.Tag.Get("json")
		parts := strings.Split(tag, ",")
		if parts[0] == "-" {
			continue
		}
		if f.Anonymous && parts[0] == "" {
			base := f.Type
			for base.Kind() == reflect.Pointer {
				base = base.Elem()
			}
			if base.Kind() == reflect.Struct {
				obj := s.object(base)
				for k, v := range obj["properties"].(Schema) {
					props[k] = v
				}
				required = append(required, obj["required"].([]string)...)
				continue
			}
		}
		name := parts[0]
		if name == "" {
			name = f.Name
		}
		props[name] = s.schema(f.Type)
		optional := false
		for _, option := range parts[1:] {
			if option == "omitempty" || option == "omitzero" {
				optional = true
			}
		}
		if !optional {
			required = append(required, name)
		}
	}
	result := Schema{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		result["required"] = required
	}
	return result
}
func Response(description string, schema Schema) Schema {
	return Schema{"description": description, "content": Schema{"application/json": Schema{"schema": schema}}}
}
func Query(name, description string, required bool, schema Schema) Schema {
	return Schema{"name": name, "in": "query", "description": description, "required": required, "schema": schema}
}
func Document(title, version, description string, paths Schema, schemas *Schemas, bearer bool) Schema {
	components := Schema{"schemas": schemas.Types}
	doc := Schema{"openapi": "3.0.3", "info": Schema{"title": title, "version": version, "description": description}, "servers": []Schema{{"url": "/"}}, "paths": paths, "components": components}
	if bearer {
		components["securitySchemes"] = Schema{"bearerAuth": Schema{"type": "http", "scheme": "bearer", "description": "ROMMETA_API_TOKEN (32+ characters). Enter the token without the Bearer prefix."}}
		doc["security"] = []Schema{{"bearerAuth": []string{}}}
	}
	return doc
}
