package gent

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"time"
)

// EncodedValue preserves concrete types across process boundaries. Ordinary JSON decoding of
// interface fields loses content-part implementations, numeric types, and typed tool metadata.
type EncodedValue struct {
	Type    string          `json:"type"`
	Version int             `json:"version"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// ValueCodec supplies a versioned representation for an application-owned value.
type ValueCodec interface {
	Type() string
	Version() int
	Encode(any) (json.RawMessage, error)
	Decode(json.RawMessage) (any, error)
}

type registeredValue struct {
	id      string
	version int
	typ     reflect.Type
	codec   ValueCodec
}

// CodecRegistry binds wire names to concrete types. Register custom types before starting a run;
// decoding never loads code or guesses a Go type from an untrusted checkpoint.
type CodecRegistry struct {
	mu     sync.RWMutex
	byType map[reflect.Type]registeredValue
	byId   map[string]registeredValue
}

func NewCodecRegistry() *CodecRegistry {
	r := &CodecRegistry{
		byType: map[reflect.Type]registeredValue{},
		byId:   map[string]registeredValue{},
	}
	values := []any{false, "", int(0), int8(0), int16(0), int32(0), int64(0), uint(0), uint8(0),
		uint16(0), uint32(0), uint64(0), float32(0), float64(0), json.Number(""), time.Duration(0),
		ChatMessageType(""), RecordOrigin(""), RecordMetadataKey(""),
		MessageContentMetadataKey(
			"",
		), TextContent{}, ImageURLContent{}, BinaryContent{},
		ModelToolCall{}, FunctionCall{}, ToolCallResponse{}, CachedContent{},
		CacheControl{}, ToolChainResult{}, ToolCallResult{}, HistoryEntry{}, MessageContent{},
		Task{}, ProviderContinuation{}, RecoveryInfo{}, ContentResponse{}, ContentChoice{},
		GenerationInfo{}, CallOptions{}, MetaContent{}, ContentSource{},
		ToolDefinition{}, FunctionDefinition{}, ToolChoice{}, FunctionReference{},
		ModelMessage{}, ThinkingConfig{}, ThinkingMode(""), json.RawMessage{},
		FormattedSection{}, time.Time{}}
	for _, value := range values {
		typ := reflect.TypeOf(value)
		id := typ.PkgPath() + "/" + typ.Name()
		r.add(registeredValue{id: id, version: 1, typ: typ})
	}
	registerFrameworkCodecs(r)
	values = []any{HistoryEntryKind(""), ContextChangeKind(""), ExecutionPhase(""),
		HistorySnapshot{}, HistoryChange{}, HistoryUpdate{}, CompactionRecord{}, ContextSelection{},
		StateValue{}, RecordedEvent{}, SaveBatch{}, RunView{}, RuntimeState{}, ContextRuntime{},
		Checkpoint{}, ContextCheckpoint{}, CompatibilityManifest{}, SavedCheckpointState{},
		SavedOutcome{},
	}
	for _, value := range values {
		typ := reflect.TypeOf(value)
		r.add(registeredValue{id: typ.PkgPath() + "/" + typ.Name(), version: 1, typ: typ})
	}
	return r
}

// Marshal preserves concrete content and metadata types in portable JSON. Use it for saved runs
// and batches; plain JSON cannot restore interface-valued content without this type information.
func (r *CodecRegistry) Marshal(value any) ([]byte, error) {
	encoded, err := r.Encode(value)
	if err != nil {
		return nil, err
	}
	return json.Marshal(encoded)
}

// Unmarshal restores a Marshal result using the same versioned type registrations.
func (r *CodecRegistry) Unmarshal(data []byte, target any) error {
	destination := reflect.ValueOf(target)
	if destination.Kind() != reflect.Pointer || destination.IsNil() {
		return errors.New("gent: decode target must be a non-nil pointer")
	}
	var encoded EncodedValue
	if err := json.Unmarshal(data, &encoded); err != nil {
		return err
	}
	decoded, err := r.Decode(encoded)
	if err != nil {
		return err
	}
	value := reflect.ValueOf(decoded)
	if !value.IsValid() {
		destination.Elem().SetZero()
		return nil
	}
	if value.Kind() == reflect.Pointer && value.Type() != destination.Elem().Type() {
		if value.IsNil() {
			return errors.New("gent: cannot decode nil into a value")
		}
		value = value.Elem()
	}
	if !value.Type().AssignableTo(destination.Elem().Type()) {
		return fmt.Errorf("gent: decoded %s, expected %s", value.Type(), destination.Elem().Type())
	}
	destination.Elem().Set(value)
	return nil
}

func cloneOwned[T any](registry *CodecRegistry, value T) (T, error) {
	encoded, err := registry.Encode(value)
	if err != nil {
		var zero T
		return zero, err
	}
	return decodeAs[T](registry, encoded)
}

func (r *CodecRegistry) add(value registeredValue) {
	r.byType[value.typ] = value
	r.byId[value.id] = value
}

// RegisterType registers a struct using its exported fields and the registry's recursive codecs.
// A caller-supplied version identifies semantic changes that Go reflection cannot detect.
func (r *CodecRegistry) RegisterType(id string, version int, sample any) error {
	return r.register(id, version, reflect.TypeOf(sample), nil)
}

func (r *CodecRegistry) Register(sample any, codec ValueCodec) error {
	if codec == nil {
		return errors.New("gent: nil value codec")
	}
	return r.register(codec.Type(), codec.Version(), reflect.TypeOf(sample), codec)
}

func (r *CodecRegistry) register(id string, version int, typ reflect.Type, codec ValueCodec) error {
	if id == "" || version < 1 || typ == nil {
		return errors.New("gent: invalid codec registration")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.byId[id]; ok {
		return fmt.Errorf("gent: codec %q already registered for %s", id, old.typ)
	}
	if old, ok := r.byType[typ]; ok {
		return fmt.Errorf("gent: type %s already registered as %q", typ, old.id)
	}
	r.add(registeredValue{id: id, version: version, typ: typ, codec: codec})
	return nil
}

func (r *CodecRegistry) lookupType(typ reflect.Type) (registeredValue, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.byType[typ]
	return value, ok
}

func (r *CodecRegistry) lookupId(id string) (registeredValue, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.byId[id]
	return value, ok
}

// Encode takes ownership by returning newly allocated bytes. Cyclic values are rejected rather
// than retained as pointers into caller-owned state.
func (r *CodecRegistry) Encode(value any) (EncodedValue, error) {
	return r.encode(reflect.ValueOf(value), make(map[any]bool), 0)
}

func encoded(id string, value any) (EncodedValue, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return EncodedValue{}, err
	}
	return EncodedValue{Type: id, Version: 1, Data: data}, nil
}

type encodedMapItem struct {
	Key   EncodedValue `json:"key"`
	Value EncodedValue `json:"value"`
}

type encodedContainer struct {
	Element string           `json:"element,omitempty"`
	Key     string           `json:"key,omitempty"`
	Nil     bool             `json:"nil,omitempty"`
	Values  []EncodedValue   `json:"values,omitempty"`
	Items   []encodedMapItem `json:"items,omitempty"`
}

func (r *CodecRegistry) encode(
	v reflect.Value,
	visiting map[any]bool,
	depth int,
) (EncodedValue, error) {
	if depth > 100 {
		return EncodedValue{}, errors.New("gent: value nesting exceeds 100")
	}
	if !v.IsValid() {
		return encoded("nil", nil)
	}
	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			return encoded("nil", nil)
		}
		return r.encode(v.Elem(), visiting, depth+1)
	}
	registration, registered := r.lookupType(v.Type())
	// Encoded payloads are already portable. Re-encoding each RawMessage byte through reflection
	// would multiply streamed event sizes and obscure the versioned payload they contain.
	if v.Type() == reflect.TypeFor[EncodedValue]() {
		return encoded("encoded-value", v.Interface())
	}
	if v.Type() == reflect.TypeFor[json.RawMessage]() {
		if v.IsNil() {
			return encoded("raw-json-nil", nil)
		}
		if !json.Valid(v.Bytes()) {
			return EncodedValue{}, errors.New("gent: invalid raw JSON")
		}
		// JSON marshaling rewrites RawMessage whitespace and escapes. Save bytes so opaque
		// provider items retain their exact representation through copies and saved runs.
		return encoded("raw-json-bytes", v.Bytes())
	}
	if v.CanInterface() && (!registered || registration.codec == nil) {
		if err, ok := v.Interface().(error); ok {
			return encoded("error", SaveError(err))
		}
		if t, ok := v.Interface().(time.Time); ok {
			return encoded("time", t)
		}
	}
	if registered && registration.codec != nil {
		data, err := registration.codec.Encode(v.Interface())
		if err != nil {
			return EncodedValue{}, fmt.Errorf("encode %s: %w", registration.id, err)
		}
		if !json.Valid(data) {
			return EncodedValue{}, fmt.Errorf("codec %s returned invalid JSON", registration.id)
		}
		return EncodedValue{
			registration.id,
			registration.version,
			append(json.RawMessage(nil), data...),
		}, nil
	}
	if v.Kind() == reflect.Pointer || v.Kind() == reflect.Map || v.Kind() == reflect.Slice {
		if !v.IsNil() {
			key := struct {
				typ     reflect.Type
				pointer uintptr
			}{v.Type(), v.Pointer()}
			if visiting[key] {
				return EncodedValue{}, fmt.Errorf("gent: cyclic value %s", v.Type())
			}
			visiting[key] = true
			defer delete(visiting, key)
		}
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
		if v.Type().Name() != "" && !registered {
			return EncodedValue{}, fmt.Errorf("gent: no codec for named container %s", v.Type())
		}
		container, err := r.encodeContainer(v, visiting, depth)
		if err != nil || !registered {
			return container, err
		}
		value, err := encoded(registration.id, container)
		value.Version = registration.version
		return value, err
	case reflect.Struct:
		if !registered {
			return EncodedValue{}, fmt.Errorf("gent: no codec for %s", v.Type())
		}
		fields := map[string]EncodedValue{}
		for i := 0; i < v.NumField(); i++ {
			field := v.Type().Field(i)
			if field.PkgPath != "" {
				return EncodedValue{}, fmt.Errorf(
					"gent: unexported field %s.%s",
					v.Type(),
					field.Name,
				)
			}
			if field.Type.Kind() == reflect.Func && field.Tag.Get("json") == "-" {
				if !v.Field(i).IsNil() {
					return EncodedValue{}, fmt.Errorf(
						"gent: callback %s is not serializable",
						field.Name,
					)
				}
				continue
			}
			value, err := r.encode(v.Field(i), visiting, depth+1)
			if err != nil {
				return EncodedValue{}, fmt.Errorf("%s.%s: %w", v.Type(), field.Name, err)
			}
			fields[field.Name] = value
		}
		value, err := encoded(registration.id, fields)
		value.Version = registration.version
		return value, err
	case reflect.Float32, reflect.Float64:
		if math.IsInf(v.Float(), 0) || math.IsNaN(v.Float()) {
			return EncodedValue{}, errors.New("gent: non-finite number")
		}
		fallthrough
	case reflect.Bool, reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32,
		reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if !registered {
			return EncodedValue{}, fmt.Errorf("gent: no codec for %s", v.Type())
		}
		value, err := encoded(registration.id, v.Interface())
		value.Version = registration.version
		return value, err
	default:
		return EncodedValue{}, fmt.Errorf("gent: unsupported value %s", v.Type())
	}
}

func (r *CodecRegistry) typeName(t reflect.Type) (string, error) {
	if t.Kind() == reflect.Interface {
		if t.NumMethod() == 0 {
			return "any", nil
		}
		if t == reflect.TypeFor[ContentPart]() {
			return "content", nil
		}
		if t == reflect.TypeFor[error]() {
			return "error-interface", nil
		}
	}
	if value, ok := r.lookupType(t); ok {
		return value.id, nil
	}
	if t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Map ||
		t.Kind() == reflect.Array {
		element, err := r.typeName(t.Elem())
		if err != nil {
			return "", err
		}
		if t.Kind() == reflect.Pointer {
			return "*" + element, nil
		}
		if t.Kind() == reflect.Slice {
			return "[]" + element, nil
		}
		if t.Kind() == reflect.Array {
			return fmt.Sprintf("[%d]%s", t.Len(), element), nil
		}
		key, err := r.typeName(t.Key())
		if err != nil {
			return "", err
		}
		data, err := json.Marshal([]string{key, element})
		return "map:" + string(data), err
	}
	return "", fmt.Errorf("gent: no codec for container element %s", t)
}

func (r *CodecRegistry) encodeContainer(
	v reflect.Value,
	visiting map[any]bool,
	depth int,
) (EncodedValue, error) {
	name, err := r.typeName(v.Type().Elem())
	if err != nil {
		return EncodedValue{}, err
	}
	c := encodedContainer{Element: name}
	if v.Kind() != reflect.Array {
		c.Nil = v.IsNil()
	}
	switch v.Kind() {
	case reflect.Pointer:
		if !c.Nil {
			item, err := r.encode(v.Elem(), visiting, depth+1)
			if err != nil {
				return EncodedValue{}, err
			}
			c.Values = []EncodedValue{item}
		}
		return encoded("pointer", c)
	case reflect.Map:
		c.Key, err = r.typeName(v.Type().Key())
		if err != nil {
			return EncodedValue{}, err
		}
		iter := v.MapRange()
		for iter.Next() {
			key, err := r.encode(iter.Key(), visiting, depth+1)
			if err != nil {
				return EncodedValue{}, err
			}
			value, err := r.encode(iter.Value(), visiting, depth+1)
			if err != nil {
				return EncodedValue{}, err
			}
			c.Items = append(c.Items, encodedMapItem{key, value})
		}
		// Map iteration order cannot change a batch digest between equivalent encodings.
		sortEncodedItems(c.Items)
		return encoded("map", c)
	default:
		for i := 0; i < v.Len(); i++ {
			item, err := r.encode(v.Index(i), visiting, depth+1)
			if err != nil {
				return EncodedValue{}, err
			}
			c.Values = append(c.Values, item)
		}
		if v.Kind() == reflect.Array {
			return encoded("array", c)
		}
		return encoded("slice", c)
	}
}

// Decode validates every type and version before constructing an owned Go value.
func (r *CodecRegistry) Decode(value EncodedValue) (any, error) {
	v, err := r.decode(value, 0)
	if err != nil {
		return nil, err
	}
	if !v.IsValid() {
		return nil, nil
	}
	return v.Interface(), nil
}

func (r *CodecRegistry) Clone(value any) (any, error) {
	encoded, err := r.Encode(value)
	if err != nil {
		return nil, err
	}
	return r.Decode(encoded)
}
