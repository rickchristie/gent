package gent

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSavedStructEvolution(t *testing.T) {
	type oldDetail struct{ Code, Removed string }
	type newDetail struct {
		Code  string
		Added int
	}
	type removedType struct{ Private string }
	type oldInput struct {
		ID      string `json:"same"`
		Removed removedType
		Detail  *oldDetail
	}
	type newInput struct {
		ID     string `json:"changed"`
		Added  bool
		Detail *newDetail
	}
	type renamedInput struct {
		Renamed string `json:"same"`
	}
	type changedInput struct{ ID any }
	type changedNumber struct{ ID int64 }
	type input struct {
		current any
		child   any
	}
	type expected struct {
		value any
		err   string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"added removed and nested", input{newInput{}, newDetail{}}, expected{
			newInput{ID: "O1", Detail: &newDetail{Code: "current"}}, ""}},
		{"rename does not follow a JSON tag", input{renamedInput{}, nil},
			expected{renamedInput{}, ""}},
		{"assignable type change is rejected", input{changedInput{}, nil},
			expected{nil, "gent: changed declared type for test/input.ID"}},
		{"numeric conversion is rejected", input{changedNumber{}, nil},
			expected{nil, "gent: changed declared type for test/input.ID"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			old := NewCodecRegistry()
			assert.NoError(t, old.RegisterType("test/input", 1, oldInput{}))
			assert.NoError(t, old.RegisterType("test/detail", 1, oldDetail{}))
			assert.NoError(t, old.RegisterType("test/removed", 1, removedType{}))
			encoded, err := old.Encode(oldInput{ID: "O1", Removed: removedType{"saved"},
				Detail: &oldDetail{Code: "current", Removed: "old"}})
			assert.NoError(t, err)
			before, err := json.Marshal(encoded)
			assert.NoError(t, err)
			fresh := NewCodecRegistry()
			assert.NoError(t, fresh.RegisterType("test/input", 1, test.input.current))
			if test.input.child != nil {
				assert.NoError(t, fresh.RegisterType("test/detail", 1, test.input.child))
			}
			// The removed field has no codec in this process and must not be decoded.
			value, err := fresh.Decode(encoded)
			actual := expected{value: value}
			if err != nil {
				actual.err = err.Error()
			}
			assert.Equal(t, test.expected, actual)
			after, err := json.Marshal(encoded)
			assert.NoError(t, err)
			assert.Equal(t, before, after, "typed decoding must retain the saved evidence")
		})
	}
}

func TestSavedAnonymousStructEvolution(t *testing.T) {
	type oldElement struct {
		Items []struct{ ID, Removed string }
	}
	type newElement struct {
		Items []struct {
			ID    string
			Added bool
		}
	}
	type changedElement struct {
		Items []struct{ ID any }
	}
	type input struct{ current any }
	type expected struct {
		value any
		err   string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"nested fields", input{newElement{}}, expected{newElement{Items: []struct {
			ID    string
			Added bool
		}{{ID: "O1"}}}, ""}},
		{"nested type change", input{changedElement{}}, expected{nil,
			"test/anonymous.Items: gent: changed declared type for " +
				"go/struct { ID string; Removed string }.ID"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			old := NewCodecRegistry()
			assert.NoError(t, old.RegisterType("test/anonymous", 1, oldElement{}))
			assert.NoError(t, old.RegisterValueType(reflect.TypeFor[oldElement]().Field(0).Type))
			encoded, err := old.Encode(oldElement{Items: []struct{ ID, Removed string }{{"O1", "old"}}})
			assert.NoError(t, err)
			fresh := NewCodecRegistry()
			// Use one stable root codec ID, as the same named Go type would have across deployments.
			assert.NoError(t, fresh.RegisterType("test/anonymous", 1, test.input.current))
			assert.NoError(t, fresh.RegisterValueType(reflect.TypeOf(test.input.current).Field(0).Type))
			value, err := fresh.Decode(encoded)
			actual := expected{value: value}
			if err != nil {
				actual.err = err.Error()
			}
			assert.Equal(t, test.expected, actual)
		})
	}
}

func TestSavedStructMapKeyEvolution(t *testing.T) {
	type oldInput struct {
		Items map[struct{ ID, Removed string }]string
	}
	type newInput struct {
		Items map[struct {
			ID    string
			Added bool
		}]string
	}
	type input struct {
		items map[struct{ ID, Removed string }]string
	}
	type expected struct {
		value any
		err   string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"nested map key", input{map[struct{ ID, Removed string }]string{
			{ID: "O1", Removed: "old"}: "receipt",
		}}, expected{newInput{Items: map[struct {
			ID    string
			Added bool
		}]string{{ID: "O1"}: "receipt"}}, ""}},
		{"removed field creates duplicate key", input{map[struct{ ID, Removed string }]string{
			{ID: "O1", Removed: "first"}:  "first receipt",
			{ID: "O1", Removed: "second"}: "second receipt",
		}}, expected{nil, "test/input.Items: gent: duplicate map key"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			old := NewCodecRegistry()
			assert.NoError(t, old.RegisterType("test/input", 1, oldInput{}))
			assert.NoError(t, old.RegisterType("test/key", 1, struct{ ID, Removed string }{}))
			encoded, err := old.Encode(oldInput{Items: test.input.items})
			assert.NoError(t, err)
			fresh := NewCodecRegistry()
			assert.NoError(t, fresh.RegisterType("test/input", 1, newInput{}))
			assert.NoError(t, fresh.RegisterType("test/key-current", 1, struct {
				ID    string
				Added bool
			}{}))
			value, err := fresh.Decode(encoded)
			actual := expected{value: value}
			if err != nil {
				actual.err = err.Error()
			}
			assert.Equal(t, test.expected, actual)
		})
	}
}
