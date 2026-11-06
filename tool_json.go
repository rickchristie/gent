package gent

import "encoding/json"

// MarshalJSON exposes captured data for diagnostic JSON, such as a trace or UI snapshot.
// Provider adapters still choose their native data representation. Use CodecRegistry for saved
// execution state; ordinary JSON does not retain all captured numeric type information.
func (p DataContent) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct{ Data any }{Data: p.value})
}

// MarshalJSON exposes a reply's identity, status metadata, and prepared parts for diagnostics.
// This is not provider wire content. Adapters must use their native role and call-ID mapping and
// send only prepared parts as model content. Use CodecRegistry to save and restore this type.
func (r ModelToolCallResponse) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.data)
}

// MarshalJSON exposes the complete record for application diagnostics, including private saved
// errors and rejected returns. Never use this record as model content; use Response instead.
// Trace redactors can select or remove fields before display. CodecRegistry owns state recovery.
func (r ToolCallRecord) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.data)
}
