// Package testutil holds helpers shared by tests in more than one package.
package testutil

import "reflect"

// FillFields sets every string to "x" and every slice to one filled element,
// recursively, so each exported field of v is non-zero. Tests use it to check
// that a conversion carries every field of a struct.
func FillFields(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		v.SetString("x")
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		FillFields(v.Index(0))
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				FillFields(v.Field(i))
			}
		}
	}
}
