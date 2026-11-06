# Ownership and copies

Gent does not copy values only to protect against prohibited caller edits. A method that retains
a value documents ownership transfer. A getter that returns framework state documents a read-only
view. A caller must not change a transferred value through an old map, slice, pointer, or byte alias.
Private fields and getters make this contract visible; they do not make Go memory immutable.

| Boundary | Contract and reason |
| --- | --- |
| DataPart / PartsBuilder.Data | Capture portable data now. Later processing must use that observation. |
| BytesPart and media helpers | Transfer the buffer. No defensive byte copy. |
| Tool return → accepted record | Transfer the response. Prepare a new parts slice to add guidance; share payloads. |
| Record / response / schema getters | Read-only views. No deep copy on each read. |
| NewHistoryEntry → UpdateHistory | Transfer a proposal. It becomes read-only when accepted. |
| History getter | Read-only accepted entries and payloads. Later appends retain the prior view. |
| CompactionInput | Independent mutable entries so a strategy can construct replacements. |
| Managed request → request hook | Independent mutable request so edits do not change history or shared settings. |
| Hook return → adapter | Transfer the effective request. Saved event bytes already preserve it. |
| Adapter media preparation | Copy changed containers only. Preserve historical parts and status metadata. |
| CheckpointState / SaveBatch | Encode the value at that control boundary. Live execution can then advance. |
| Export | Independent mutable run snapshot, safe while recording continues. |
| Stream producer → live delivery | Capture a stable chunk before producer buffers or accumulators advance. |
| Stream.Response | Read-only completed response. A consumer must request a copy before editing it. |

JavaScript data objects are a language conversion, not the original Go objects. Byte views borrow
owned buffers as read-only Uint8Array values. Scripts must not alter them. Forwarding selects the
original part from the same script; it does not copy bytes or store a new reference for later use.

Without a request hook, managed model calls share read-only message and media payloads. The event
registry reports whether such a hook exists. A custom dispatcher can supply the same capability;
otherwise Gent gives it an independent mutable request. After-call observers need no request copy.

Copies at mutable hook, compaction, stream snapshot, and storage boundaries must cover the values
that can change independently. A copied slice header alone does not isolate a mutable byte buffer.
Read-only workers can share payloads. Workers that change a value need an independent mutable view.

Use CodecRegistry.Clone or CloneContentParts when an application explicitly needs such a copy.
Do not call these helpers at every internal boundary. Keep original outcomes, prepared historical
replies, and request-specific transformations distinct. Resume uses saved replies unchanged.
