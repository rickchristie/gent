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
	case TextContent, ImageURLContent, ToolCallResponse:
		return part
	case *TextContent:
		copy := *part
		return &copy
	case *ImageURLContent:
		copy := *part
		return &copy
	case *ToolCallResponse:
		copy := *part
		return &copy
	case BinaryContent:
		part.Data = cloneBytes(part.Data)
		return part
	case *BinaryContent:
		copy := *part
		copy.Data = cloneBytes(part.Data)
		return &copy
	case ModelToolCall:
		if part.FunctionCall != nil {
			copy := *part.FunctionCall
			part.FunctionCall = &copy
		}
		return part
	case *ModelToolCall:
		copy := cloneContentPart(*part).(ModelToolCall)
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
