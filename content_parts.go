package gent

import "strings"

// ContentText returns a text preview. It does not convert or validate media. Output processors
// must inspect all parts instead of using this helper to discard unsupported content.
func ContentText(parts []ContentPart) string {
	var result strings.Builder
	for _, part := range parts {
		switch part := part.(type) {
		case TextContent:
			result.WriteString(part.Text)
		case *TextContent:
			result.WriteString(part.Text)
		}
	}
	return result.String()
}

// CloneContentParts owns each part, including its nested bytes and native tool data. ContentPart
// is a closed set, so stream snapshots do not need reflection or serialization for every delta.
func CloneContentParts(parts []ContentPart) []ContentPart {
	if parts == nil {
		return nil
	}
	owned := make([]ContentPart, len(parts))
	for i, part := range parts {
		owned[i] = cloneContentPart(part)
	}
	return owned
}

func cloneContentPart(part ContentPart) ContentPart {
	switch part := part.(type) {
	case TextContent, ImageURLContent:
		return part
	case *TextContent:
		copy := *part
		return &copy
	case *ImageURLContent:
		copy := *part
		return &copy
	case *ModelToolCallResponse:
		copy := *part
		copy.data.Parts = CloneContentParts(part.Parts())
		return &copy
	case DataContent:
		return DataContent{value: copyToolSchemaValue(part.value)}
	case *DataContent:
		return &DataContent{value: copyToolSchemaValue(part.value)}
	case BinaryContent:
		part.Data = cloneBytes(part.Data)
		return part
	case *BinaryContent:
		copy := *part
		copy.Data = cloneBytes(part.Data)
		return &copy
	case ModelToolCallRequest:
		if part.FunctionCall != nil {
			copy := *part.FunctionCall
			part.FunctionCall = &copy
		}
		return part
	case *ModelToolCallRequest:
		copy := cloneContentPart(*part).(ModelToolCallRequest)
		return &copy
	case CachedContent:
		part.ContentPart = cloneContentPart(part.ContentPart)
		if part.CacheControl != nil {
			copy := *part.CacheControl
			part.CacheControl = &copy
		}
		return part
	case *CachedContent:
		copy := cloneContentPart(*part).(CachedContent)
		return &copy
	default:
		panic("gent: unsupported content part")
	}
}

func cloneBytes(value []byte) []byte {
	if value == nil {
		return nil
	}
	return append(make([]byte, 0, len(value)), value...)
}

// Mutable request hooks need an independent data tree while history keeps the captured value.
func copyToolSchemaValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, item := range value {
			result[key] = copyToolSchemaValue(item)
		}
		return result
	case []any:
		result := make([]any, len(value))
		for i, item := range value {
			result[i] = copyToolSchemaValue(item)
		}
		return result
	default:
		return value
	}
}
