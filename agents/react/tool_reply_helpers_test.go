package react

import "github.com/rickchristie/gent"

func preparedTestReply(id, name, text string) *gent.ModelToolCallResponse {
	execution := &gent.ToolExecutionResponse{
		Status: gent.ToolExecuted, Parts: []gent.ContentPart{gent.TextPart(text)},
	}
	record, err := gent.NewToolCallRecordBuilder("record-"+id, "test", name).Execution(execution).
		ModelReply(id, name, execution.Parts...).Build()
	if err != nil {
		panic(err)
	}
	return record.Response()
}
func mustTestDataPart(value any) gent.ContentPart {
	part, err := gent.DataPart(value)
	if err != nil {
		panic(err)
	}
	return part
}
