package httpapi

import (
	"net/http"
	"reflect"
)

// route is one row of the API table. The same row registers the handler
// and describes the operation in the OpenAPI document.
type route struct {
	id      string
	method  string
	path    string
	slug    string
	summary string
	params  []param
	body    reflect.Type
	result  reflect.Type
	public  bool
	// serve answers with the JSON envelope; stream owns the response instead.
	serve  func(http.ResponseWriter, *http.Request) (any, error)
	stream http.HandlerFunc
}

type param struct {
	name        string
	in          string
	description string
	schema      map[string]any
	required    bool
}

func pathParam(name, description string, schema map[string]any) param {
	return param{name: name, in: "path", description: description, schema: schema, required: true}
}

func queryParam(name, description string, schema map[string]any) param {
	return param{name: name, in: "query", description: description, schema: schema}
}

var (
	stringSchema = map[string]any{"type": "string"}
	idSchema     = map[string]any{"type": "integer", "format": "int64", "minimum": 1}
	intSchema    = map[string]any{"type": "integer", "format": "int32"}
	listSchema   = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
)

// query declares a route whose input comes from the path and query string.
func query[Out any](id, method, path, slug, summary string, params []param, fn func(*http.Request) (Out, error)) route {
	return route{
		id: id, method: method, path: path, slug: slug, summary: summary, params: params,
		result: reflect.TypeFor[Out](),
		serve: func(_ http.ResponseWriter, r *http.Request) (any, error) {
			return fn(r)
		},
	}
}

// command declares a route that also decodes a JSON body of type In.
func command[In, Out any](id, method, path, slug, summary string, params []param, fn func(*http.Request, In) (Out, error)) route {
	return route{
		id: id, method: method, path: path, slug: slug, summary: summary, params: params,
		body:   reflect.TypeFor[In](),
		result: reflect.TypeFor[Out](),
		serve: func(w http.ResponseWriter, r *http.Request) (any, error) {
			var in In
			if err := decodeBody(w, r, &in); err != nil {
				return nil, err
			}
			return fn(r, in)
		},
	}
}
