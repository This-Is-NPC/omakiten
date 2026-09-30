package httpapi

import (
	"encoding/json"
	"reflect"
	"strings"

	"omakiten/internal/output"
)

// buildSpec derives the OpenAPI 3.1 document from the route table and the
// Go types each route decodes and returns.
func buildSpec(routes []route) ([]byte, error) {
	schemas := newSchemas()
	failure := map[string]any{
		"description": "Error envelope.",
		"content":     map[string]any{"application/json": map[string]any{"schema": schemas.of(reflect.TypeFor[output.Envelope]())}},
	}
	paths := map[string]map[string]any{}
	for _, rt := range routes {
		if paths[rt.path] == nil {
			paths[rt.path] = map[string]any{}
		}
		paths[rt.path][strings.ToLower(rt.method)] = specOperation(rt, schemas, failure)
	}
	doc := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   "Omakiten local API",
			"version": "1",
		},
		"security": []any{map[string]any{"bearer": []any{}}},
		"paths":    paths,
		"components": map[string]any{
			"schemas":         schemas.components,
			"securitySchemes": map[string]any{"bearer": map[string]any{"type": "http", "scheme": "bearer"}},
		},
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func specOperation(rt route, schemas *schemas, failure map[string]any) map[string]any {
	op := map[string]any{"operationId": rt.id, "summary": rt.summary}
	if rt.slug != "" {
		op["x-okt-surface"] = rt.slug
	}
	if rt.public {
		op["security"] = []any{}
	}
	if len(rt.params) > 0 {
		params := make([]any, 0, len(rt.params))
		for _, p := range rt.params {
			params = append(params, map[string]any{"name": p.name, "in": p.in, "description": p.description, "required": p.required, "schema": p.schema})
		}
		op["parameters"] = params
	}
	if rt.body != nil {
		op["requestBody"] = map[string]any{
			"required": true,
			"content":  map[string]any{"application/json": map[string]any{"schema": schemas.of(rt.body)}},
		}
	}
	success := map[string]any{"description": "Success."}
	if rt.stream != nil {
		success["content"] = map[string]any{"text/event-stream": map[string]any{"schema": schemas.of(rt.result)}}
	} else {
		success["content"] = map[string]any{"application/json": map[string]any{"schema": map[string]any{
			"type":     "object",
			"required": []string{"ok", "data"},
			"properties": map[string]any{
				"ok":   map[string]any{"const": true},
				"data": schemas.of(rt.result),
			},
		}}}
	}
	op["responses"] = map[string]any{"200": success, "default": failure}
	return op
}
