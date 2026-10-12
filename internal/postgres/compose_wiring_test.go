package postgres

import (
	"reflect"
	"testing"

	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
)

// The store is composed of storage packages that hold each other by pointer.
// A pointer that the composition left nil fails only when its method runs.
// This test walks the composed store and each package that it holds.
func TestComposedStoreHoldsEveryStoragePackage(t *testing.T) {
	var walk func(path string, v reflect.Value)
	walk = func(path string, v reflect.Value) {
		for i := 0; i < v.NumField(); i++ {
			field := v.Type().Field(i)
			if !field.Anonymous {
				continue
			}
			value := v.Field(i)
			if value.Kind() == reflect.Ptr {
				if value.IsNil() {
					t.Errorf("%s.%s is nil", path, field.Name)
					continue
				}
				value = value.Elem()
			}
			if value.Kind() == reflect.Struct && field.Type != reflect.TypeOf(&kernel.DB{}) {
				walk(path+"."+field.Name, value)
			}
		}
	}
	walk("Store", reflect.ValueOf(newStore(&kernel.DB{})).Elem())
}
