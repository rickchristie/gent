package tt

import (
	"reflect"

	"github.com/rickchristie/gent"
)

// Value makes a portable fixture value. Invalid fixture types fail at construction, before a
// test can mistake an encoding error for the behavior it intends to check.
func Value(value any) *gent.EncodedValue {
	registry := gent.NewCodecRegistry()
	if value != nil {
		if err := registry.RegisterValueType(reflect.TypeOf(value)); err != nil {
			panic(err)
		}
	}
	encoded, err := registry.Encode(value)
	if err != nil {
		panic(err)
	}
	return &encoded
}
