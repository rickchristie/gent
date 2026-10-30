package trace

import "github.com/rickchristie/gent"

// Display limits never cut encoded media into invalid files. Omit an oversized part from the
// bounded preview and mark it truncated; accepted run history still retains the complete part.
func limitContent(parts []gent.ContentPart, remaining int) ([]gent.ContentPart, bool) {
	if remaining < 0 {
		return gent.CloneContentParts(parts), false
	}
	var result []gent.ContentPart
	truncated := false
	for _, part := range parts {
		switch part := part.(type) {
		case gent.TextContent:
			text, cut := appendLimitedUTF8("", part.Text, remaining)
			if text != "" {
				result = append(result, gent.TextContent{Text: text})
			}
			truncated = truncated || cut
			remaining -= len(text)
		case *gent.TextContent:
			text, cut := appendLimitedUTF8("", part.Text, remaining)
			if text != "" {
				result = append(result, &gent.TextContent{Text: text})
			}
			truncated = truncated || cut
			remaining -= len(text)
		default:
			size := contentSize([]gent.ContentPart{part})
			if size > remaining {
				truncated = true
				continue
			}
			result = append(result, gent.CloneContentParts([]gent.ContentPart{part})...)
			remaining -= size
		}
	}
	return result, truncated
}

func contentSize(parts []gent.ContentPart) int {
	var size int
	for _, part := range parts {
		switch part := part.(type) {
		case gent.TextContent:
			size += len(part.Text)
		case *gent.TextContent:
			size += len(part.Text)
		case gent.BinaryContent:
			size += len(part.Data)
		case *gent.BinaryContent:
			size += len(part.Data)
		case gent.ImageURLContent:
			size += len(part.URL)
		case *gent.ImageURLContent:
			size += len(part.URL)
		}
	}
	return size
}
