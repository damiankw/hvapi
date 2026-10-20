package server

import "reflect"

// normalize makes the JSON contract hold: a slice or map field that is nil
// in Go would encode as null, but the OpenAPI document (which marks every
// field without omitempty as required) promises an array or object there.
// It replaces nil slices and maps with empty ones, recursively, so clients
// can rely on "required" meaning "never null".
//
// It takes any value and returns one safe to encode. Pointers are walked in
// place; values are copied first.
func normalize(v any) any {
	if v == nil {
		return v
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		if !rv.IsNil() {
			normalizeValue(rv.Elem())
		}
		return v
	}
	c := reflect.New(rv.Type()).Elem()
	c.Set(rv)
	normalizeValue(c)
	return c.Interface()
}

func normalizeValue(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			normalizeValue(v.Elem())
		}
	case reflect.Interface:
		if v.IsNil() || !v.CanSet() {
			return
		}
		e := v.Elem()
		if e.Kind() == reflect.Pointer {
			normalizeValue(e)
			return
		}
		c := reflect.New(e.Type()).Elem()
		c.Set(e)
		normalizeValue(c)
		v.Set(c)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if f := v.Field(i); f.CanSet() {
				normalizeValue(f)
			}
		}
	case reflect.Slice:
		if v.IsNil() {
			if v.CanSet() {
				v.Set(reflect.MakeSlice(v.Type(), 0, 0))
			}
			return
		}
		for i := 0; i < v.Len(); i++ {
			normalizeValue(v.Index(i))
		}
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			normalizeValue(v.Index(i))
		}
	case reflect.Map:
		if v.IsNil() {
			if v.CanSet() {
				v.Set(reflect.MakeMap(v.Type()))
			}
			return
		}
		for _, k := range v.MapKeys() {
			e := v.MapIndex(k)
			switch e.Kind() {
			case reflect.Struct, reflect.Slice, reflect.Map, reflect.Pointer, reflect.Interface:
				c := reflect.New(e.Type()).Elem()
				c.Set(e)
				normalizeValue(c)
				v.SetMapIndex(k, c)
			}
		}
	}
}
