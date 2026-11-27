// Package citation holds the one shape a cited document takes everywhere: in
// search hits, chat answers, batch results, saved chats, and the daemon's wire
// format. It has no dependencies, so the knowledge, chat, chatstore and
// apiclient packages can all share it.
package citation

// Citation identifies the document a search hit came from, in a form fit for
// showing to a user. It is document-level: chunks of the same source share a
// Key. Locator is reserved for a future section/cell reference and is always
// empty today.
type Citation struct {
	// Key is the cited document's identity; today the hit's source id.
	Key string `json:"key"`
	// Label is the human-readable name of the document. Never empty once
	// resolved.
	Label string `json:"label"`
	// Href is a clickable link to the document, empty when none is recorded.
	Href    string `json:"href,omitempty"`
	Locator string `json:"locator,omitempty"`
	// Base is the knowledge-base name the hit came from ("kapa" for Kapa hits).
	Base string `json:"base"`
	// Tag is the hit's resolved knowledge label (e.g. "canonical").
	Tag string `json:"tag"`
	// Via records which resolvers supplied the fields, for verbose output.
	Via string `json:"-"`
}

// Source is a cited document as presented to a user: the marker number it
// carries in the answer (the n of "[n]") plus its resolved citation. It is the
// shape of a source in websocket frames, batch results, and saved chats.
type Source struct {
	Marker int `json:"marker"`
	Citation
}
