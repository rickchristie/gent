//go:build ignore

// Generate the small ONNX model for the AVX2 overflow regression test.
// Run with go generate ./search from the repository root.
package main

import (
	"log"
	"os"

	"google.golang.org/protobuf/encoding/protowire"
)

const (
	onnxFloat = 1
	onnxUint8 = 2
	onnxInt8  = 3
	onnxInt64 = 7
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: go run generate_quantized_matmul.go OUTPUT.onnx")
	}

	// Token 1 becomes [255, 255], then [64770, 32640] after MatMulInteger. An AVX2
	// kernel using saturating INT16 intermediate sums incorrectly returns 32767 in
	// the first dimension. Input-dependent activations prevent constant folding.
	var graph message
	graph.bytes(1, node("Mul", []string{"input_ids", "gain"}, "scaled"))
	graph.bytes(1, node("Cast", []string{"scaled"}, "unsigned", intAttribute("to", onnxUint8)))
	graph.bytes(1, node("Unsqueeze", []string{"unsigned", "axes"}, "column"))
	graph.bytes(1, node("Concat", []string{"column", "column"}, "activation", intAttribute("axis", 2)))
	graph.bytes(1, node(
		"MatMulInteger", []string{"activation", "weights", "a_zero", "b_zero"}, "product",
	))
	graph.bytes(1, node(
		"Cast", []string{"product"}, "last_hidden_state", intAttribute("to", onnxFloat),
	))
	graph.text(2, "quantized-matmul") // GraphProto.name
	graph.bytes(5, tensor("gain", onnxInt64, nil, []uint64{255}))
	graph.bytes(5, tensor("axes", onnxInt64, []uint64{1}, []uint64{2}))
	graph.bytes(5, tensor("weights", onnxInt8, []uint64{2, 2}, []uint64{127, 64, 127, 64}))
	graph.bytes(5, tensor("a_zero", onnxUint8, nil, []uint64{0}))
	graph.bytes(5, tensor("b_zero", onnxInt8, nil, []uint64{0}))
	graph.bytes(11, valueInfo("input_ids", onnxInt64, 1, 0))
	graph.bytes(12, valueInfo("last_hidden_state", onnxFloat, 1, 0, 2))

	var opset message
	opset.text(1, "")    // OperatorSetIdProto.domain: standard ONNX operators
	opset.integer(2, 13) // OperatorSetIdProto.version
	var model message
	model.integer(1, 8) // ModelProto.ir_version
	model.bytes(7, graph)
	model.bytes(8, opset)
	if err := os.WriteFile(os.Args[1], model, 0o644); err != nil {
		log.Fatal(err)
	}
}

func node(op string, inputs []string, output string, attributes ...message) message {
	var n message
	for _, input := range inputs {
		n.text(1, input)
	}
	n.text(2, output)
	n.text(4, op)
	for _, attribute := range attributes {
		n.bytes(5, attribute)
	}
	return n
}

func intAttribute(name string, value uint64) message {
	var a message
	a.text(1, name)
	a.integer(3, value) // AttributeProto.i
	a.integer(20, 2)    // AttributeProto.type: INT
	return a
}

func tensor(name string, dataType uint64, dimensions, values []uint64) message {
	var t message
	for _, size := range dimensions {
		t.integer(1, size)
	}
	t.integer(2, dataType)
	dataField := protowire.Number(5) // TensorProto.int32_data also stores INT8/UINT8.
	if dataType == onnxInt64 {
		dataField = 7 // TensorProto.int64_data
	}
	var packed []byte
	for _, value := range values {
		packed = protowire.AppendVarint(packed, value)
	}
	t.bytes(dataField, packed)
	t.text(8, name)
	return t
}

// A zero dimension denotes the symbolic sequence length, shared by input and output.
func valueInfo(name string, dataType uint64, dimensions ...uint64) message {
	var shape message
	for _, size := range dimensions {
		var dim message
		if size == 0 {
			dim.text(2, "length") // TensorShapeProto.Dimension.dim_param
		} else {
			dim.integer(1, size) // TensorShapeProto.Dimension.dim_value
		}
		shape.bytes(1, dim)
	}
	var tensorType message
	tensorType.integer(1, dataType)
	tensorType.bytes(2, shape)
	var valueType message
	valueType.bytes(1, tensorType) // TypeProto.tensor_type
	var v message
	v.text(1, name)
	v.bytes(2, valueType)
	return v
}

// Encode only the protobuf fields this fixture needs, using an existing dependency
// rather than adding generated ONNX bindings. Field numbers come from the ONNX schema:
// https://github.com/onnx/onnx/blob/v1.22.0/onnx/onnx.proto
type message []byte

func (m *message) integer(field protowire.Number, value uint64) {
	*m = protowire.AppendTag(*m, field, protowire.VarintType)
	*m = protowire.AppendVarint(*m, value)
}

func (m *message) bytes(field protowire.Number, value []byte) {
	*m = protowire.AppendTag(*m, field, protowire.BytesType)
	*m = protowire.AppendBytes(*m, value)
}

func (m *message) text(field protowire.Number, value string) {
	m.bytes(field, []byte(value))
}
