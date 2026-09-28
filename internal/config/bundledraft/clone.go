package bundledraft

import (
	"reflect"

	"omakiten/internal/config"
)

// CloneBundle returns a deep copy of the bundle. Every draft boundary — the
// baseline, the candidate and both halves of a report — holds its own copy so
// a caller that scribbles on one cannot reach the others.
func CloneBundle(in config.Bundle) config.Bundle {
	return deepCloneValue(reflect.ValueOf(in)).Interface().(config.Bundle)
}

var errorType = reflect.TypeFor[error]()

func deepCloneValue(value reflect.Value) reflect.Value {
	if !value.IsValid() || value.Type().Implements(errorType) {
		return value
	}
	switch value.Kind() {
	case reflect.Interface:
		return cloneInterface(value)
	case reflect.Pointer:
		return clonePointer(value)
	case reflect.Map:
		return cloneMap(value)
	case reflect.Slice:
		return cloneSlice(value)
	case reflect.Array:
		return cloneArray(value)
	case reflect.Struct:
		return cloneStruct(value)
	default:
		return value
	}
}

func cloneInterface(value reflect.Value) reflect.Value {
	if value.IsNil() {
		return reflect.Zero(value.Type())
	}
	out := reflect.New(value.Type()).Elem()
	out.Set(deepCloneValue(value.Elem()))
	return out
}

func clonePointer(value reflect.Value) reflect.Value {
	if value.IsNil() {
		return reflect.Zero(value.Type())
	}
	out := reflect.New(value.Type().Elem())
	out.Elem().Set(deepCloneValue(value.Elem()))
	return out
}

func cloneMap(value reflect.Value) reflect.Value {
	if value.IsNil() {
		return reflect.Zero(value.Type())
	}
	out := reflect.MakeMapWithSize(value.Type(), value.Len())
	iter := value.MapRange()
	for iter.Next() {
		out.SetMapIndex(deepCloneValue(iter.Key()), deepCloneValue(iter.Value()))
	}
	return out
}

func cloneSlice(value reflect.Value) reflect.Value {
	if value.IsNil() {
		return reflect.Zero(value.Type())
	}
	out := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
	for i := 0; i < value.Len(); i++ {
		out.Index(i).Set(deepCloneValue(value.Index(i)))
	}
	return out
}

func cloneArray(value reflect.Value) reflect.Value {
	out := reflect.New(value.Type()).Elem()
	for i := 0; i < value.Len(); i++ {
		out.Index(i).Set(deepCloneValue(value.Index(i)))
	}
	return out
}

func cloneStruct(value reflect.Value) reflect.Value {
	out := reflect.New(value.Type()).Elem()
	for i := 0; i < value.NumField(); i++ {
		if out.Field(i).CanSet() && value.Field(i).CanInterface() {
			out.Field(i).Set(deepCloneValue(value.Field(i)))
		}
	}
	return out
}
