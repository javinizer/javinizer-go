package models

import (
	"encoding"
	"encoding/json"
	"reflect"
	"testing"
)

func TestGeneratedFilesJSONEncodingContract(t *testing.T) {
	jsonMarshaler := reflect.TypeFor[json.Marshaler]()
	textMarshaler := reflect.TypeFor[encoding.TextMarshaler]()
	visiting := make(map[reflect.Type]bool)
	var inspect func(reflect.Type)
	inspect = func(typ reflect.Type) {
		t.Helper()
		if visiting[typ] {
			t.Fatalf("ledger type graph can contain a cycle through %v", typ)
		}
		for _, candidate := range []reflect.Type{typ, reflect.PointerTo(typ)} {
			if candidate.Implements(jsonMarshaler) || candidate.Implements(textMarshaler) {
				t.Fatalf("%v has a custom encoder whose errors need handling", candidate)
			}
		}
		visiting[typ] = true
		defer delete(visiting, typ)
		switch typ.Kind() {
		case reflect.Bool, reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		case reflect.Pointer, reflect.Slice, reflect.Array:
			inspect(typ.Elem())
		case reflect.Struct:
			for i := 0; i < typ.NumField(); i++ {
				field := typ.Field(i)
				if field.IsExported() && field.Tag.Get("json") != "-" {
					inspect(field.Type)
				}
			}
		default:
			t.Fatalf("%v is outside the infallible ledger encoding contract", typ)
		}
	}
	inspect(reflect.TypeFor[GeneratedFilesJSON]())
}
