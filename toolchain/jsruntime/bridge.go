package jsruntime

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"

	"github.com/grafana/sobek"
	"github.com/rickchristie/gent"
)

// ToolCallFn runs a native batch and returns records in request order. Run failures interrupt
// the VM. Tool operation errors are safe parts in a record, never Go errors from this callback.
type ToolCallFn func([]*gent.ModelToolCallRequest) ([]*gent.ToolCallRecord, error)

// Bridge owns the views and byte selections for one script. It has no cross-script references.
// Returned byte views are read-only. They borrow the original content under Gent's ownership rule.
type Bridge struct {
	vm        *sobek.Runtime
	call      ToolCallFn
	mode      gent.PTCIntegerMode
	records   []*gent.ToolCallRecord
	bytes     map[*sobek.Object]partPosition
	forwarded map[partPosition]bool
}

type partPosition struct {
	record *gent.ToolCallRecord
	index  int
}

// RegisterToolBridge provides the common model reply through call and parallelCall. forward
// selects original byte parts for this script's model reply. It does not start a model call.
func RegisterToolBridge(rt *Runtime, call ToolCallFn, mode gent.PTCIntegerMode) *Bridge {
	b := &Bridge{vm: rt.VM(), call: call, mode: mode,
		bytes: map[*sobek.Object]partPosition{}, forwarded: map[partPosition]bool{}}
	rt.RegisterObject("tool", map[string]func(sobek.FunctionCall) sobek.Value{
		"call": b.single, "parallelCall": b.parallel, "forward": b.forward,
	})
	return b
}

func (b *Bridge) single(call sobek.FunctionCall) sobek.Value {
	request := b.request(call.Argument(0))
	records := b.execute([]*gent.ModelToolCallRequest{request})
	if len(records) != 1 {
		panic(b.vm.NewTypeError("tool.call returned an invalid record count"))
	}
	return b.reply(records[0])
}

func (b *Bridge) parallel(call sobek.FunctionCall) sobek.Value {
	values := b.array(call.Argument(0), "tool.parallelCall")
	requests := make([]*gent.ModelToolCallRequest, len(values))
	for i, value := range values {
		requests[i] = b.request(value)
	}
	if len(requests) == 0 {
		return b.vm.NewArray()
	}
	records := b.execute(requests)
	if len(records) != len(requests) {
		panic(b.vm.NewTypeError("tool.parallelCall returned an invalid record count"))
	}
	replies := make([]any, len(records))
	for i, record := range records {
		replies[i] = b.reply(record)
	}
	return b.vm.NewArray(replies...)
}

func (b *Bridge) execute(requests []*gent.ModelToolCallRequest) []*gent.ToolCallRecord {
	records, err := b.call(requests)
	if err != nil {
		// Interrupts cannot be caught by script. Keep storage and cancellation failures private.
		b.vm.Interrupt(err)
		return nil
	}
	b.records = append(b.records, records...)
	return records
}

func (b *Bridge) request(value sobek.Value) *gent.ModelToolCallRequest {
	var request map[string]any
	if sobek.IsUndefined(value) || sobek.IsNull(value) {
		panic(b.vm.NewTypeError("Use tool.call({tool: 'name', args: {...}})."))
	}
	if err := b.vm.ExportTo(value, &request); err != nil {
		panic(b.vm.NewTypeError("Use tool.call({tool: 'name', args: {...}})."))
	}
	name, _ := request["tool"].(string)
	if name == "" {
		panic(b.vm.NewTypeError("tool.call requires a non-empty tool name."))
	}
	args := request["args"]
	if args == nil {
		args = map[string]any{}
	}
	// Sobek exports bigint as *big.Int. encoding/json keeps its exact decimal value.
	data, err := json.Marshal(args)
	if err != nil {
		panic(b.vm.NewTypeError("Tool arguments must be JSON-compatible data."))
	}
	return &gent.ModelToolCallRequest{Type: "function", FunctionCall: &gent.FunctionCall{
		Name: name, Arguments: string(data)}}
}

func (b *Bridge) reply(record *gent.ToolCallRecord) sobek.Value {
	reply := record.Response()
	parts := make([]any, len(reply.Parts()))
	for i, part := range reply.Parts() {
		view := b.part(part)
		if _, ok := binaryPart(part); ok {
			b.bytes[view] = partPosition{record, i}
		}
		parts[i] = view
	}
	result := b.vm.NewObject()
	set(result, "toolCallId", reply.ToolCallID())
	set(result, "name", reply.Name())
	set(result, "kind", string(reply.Kind()))
	if status, present := reply.ExecutionStatus(); present {
		set(result, "executionStatus", string(status))
	}
	if status, present := reply.RecoveryStatus(); present {
		set(result, "recoveryStatus", string(status))
	}
	set(result, "parts", b.vm.NewArray(parts...))
	return result
}

func (b *Bridge) part(part gent.ContentPart) *sobek.Object {
	result := b.vm.NewObject()
	switch value := part.(type) {
	case gent.CachedContent:
		return b.part(value.ContentPart)
	case *gent.CachedContent:
		return b.part(value.ContentPart)
	case gent.TextContent:
		set(result, "kind", "text")
		set(result, "text", value.Text)
	case *gent.TextContent:
		return b.part(*value)
	case gent.DataContent:
		set(result, "kind", "data")
		set(result, "data", b.data(value.Value()))
	case *gent.DataContent:
		return b.part(*value)
	case gent.ImageURLContent:
		set(result, "kind", "image_url")
		set(result, "url", value.URL)
	case *gent.ImageURLContent:
		return b.part(*value)
	case gent.BinaryContent:
		set(result, "kind", "bytes")
		set(result, "mimeType", value.MIMEType)
		bytes, err := b.vm.New(b.vm.Get("Uint8Array"), b.vm.ToValue(b.vm.NewArrayBuffer(value.Data)))
		if err != nil {
			panic(err)
		}
		set(result, "data", bytes)
	case *gent.BinaryContent:
		return b.part(*value)
	default:
		panic(b.vm.NewTypeError("Unsupported tool content part."))
	}
	return result
}

func (b *Bridge) data(value any) sobek.Value {
	switch value := value.(type) {
	case map[string]any:
		result := b.vm.NewObject()
		for key, item := range value {
			// Domain keys are own data properties, including __proto__. Assignment can invoke
			// inherited setters and change the data that a later tool receives.
			err := result.DefineDataProperty(key, b.data(item),
				sobek.FLAG_TRUE, sobek.FLAG_TRUE, sobek.FLAG_TRUE)
			if err != nil {
				panic(err)
			}
		}
		return result
	case []any:
		result := make([]any, len(value))
		for i, item := range value {
			result[i] = b.data(item)
		}
		return b.vm.NewArray(result...)
	case int64:
		if b.mode == gent.PTCIntegerBigInt {
			return b.vm.ToValue(big.NewInt(value))
		}
		return b.vm.ToValue(float64(value))
	case uint64:
		if b.mode == gent.PTCIntegerBigInt {
			return b.vm.ToValue(new(big.Int).SetUint64(value))
		}
		return b.vm.ToValue(float64(value))
	case json.Number:
		if b.mode == gent.PTCIntegerBigInt {
			if integer, ok := new(big.Int).SetString(string(value), 10); ok {
				return b.vm.ToValue(integer)
			}
		}
		number, err := value.Float64()
		if err != nil {
			panic(b.vm.NewTypeError("Data number is out of range."))
		}
		return b.vm.ToValue(number)
	default:
		return b.vm.ToValue(value)
	}
}

func (b *Bridge) array(value sobek.Value, name string) []sobek.Value {
	object, ok := value.(*sobek.Object)
	if !ok || object.ClassName() != "Array" {
		panic(b.vm.NewTypeError("%s requires an array.", name))
	}
	result := make([]sobek.Value, int(object.Get("length").ToInteger()))
	for i := range result {
		result[i] = object.Get(strconv.Itoa(i))
	}
	return result
}

func (b *Bridge) forward(call sobek.FunctionCall) sobek.Value {
	positions := b.array(call.Argument(0), "tool.forward")
	// Validate all selections before changing the output.
	selected := make([]partPosition, len(positions))
	for i, value := range positions {
		object, ok := value.(*sobek.Object)
		position, found := b.bytes[object]
		if !ok || !found {
			panic(b.vm.NewTypeError("tool.forward accepts byte parts returned in this script."))
		}
		selected[i] = position
	}
	for _, position := range selected {
		b.forwarded[position] = true
	}
	return sobek.Undefined()
}

// Parts returns all accepted replies in call order, with notices for unselected bytes. Original
// parts remain in the saved inner records. Script changes to data views do not rewrite evidence.
func (b *Bridge) Parts(steering gent.SteeringMessages) []gent.ContentPart {
	return ProgramParts(b.records, steering, func(record *gent.ToolCallRecord, index int) bool {
		return b.forwarded[partPosition{record, index}]
	})
}

// ProgramParts applies the same automatic output rule to live and interrupted programs.
// A resumed script has no active selections and therefore supplies nil for includeBytes.
func ProgramParts(records []*gent.ToolCallRecord, steering gent.SteeringMessages,
	includeBytes func(*gent.ToolCallRecord, int) bool,
) []gent.ContentPart {
	var parts []gent.ContentPart
	for _, record := range records {
		reply := record.Response()
		parts = append(parts, gent.TextPart(fmt.Sprintf("Tool %s (call %s):",
			record.Name(), reply.ToolCallID())))
		for i, part := range reply.Parts() {
			binary, isBinary := binaryPart(part)
			if !isBinary || (includeBytes != nil && includeBytes(record, i)) {
				parts = append(parts, part)
				continue
			}
			parts = append(parts, gent.TextPart(steering.Resolved().PTCBytesOmitted+
				" MIME type: "+binary.MIMEType+"."))
		}
	}
	return parts
}

func binaryPart(part gent.ContentPart) (gent.BinaryContent, bool) {
	switch part := part.(type) {
	case gent.BinaryContent:
		return part, true
	case *gent.BinaryContent:
		return *part, true
	case gent.CachedContent:
		return binaryPart(part.ContentPart)
	case *gent.CachedContent:
		return binaryPart(part.ContentPart)
	}
	return gent.BinaryContent{}, false
}

func set(object *sobek.Object, name string, value any) {
	if err := object.Set(name, value); err != nil {
		panic(err)
	}
}
