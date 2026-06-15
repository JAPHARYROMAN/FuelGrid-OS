package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Canonical snapshot serialization + hashing (Reports Center Phase 14 —
// blueprint §15).
//
// A snapshot's content_hash must be STABLE: the SAME report data captured twice
// must produce the SAME hash, so a tamper-evident snapshot can be verified and a
// re-capture of identical figures is provably identical. The rendered
// ReportEnvelope is almost entirely deterministic (every money/litre figure is an
// exact decimal string read from the repos), with ONE volatile field: the
// metadata.generated_at render timestamp, which differs on every render even for
// identical data. The canonical form therefore EXCLUDES generated_at (and any
// equally-volatile export-option URL is left as-is — those are deterministic for
// a given report/scope), then serialises with sorted keys so the byte stream is
// reproducible.
//
// canonicalEnvelopeJSON returns (a) the verbatim envelope JSON to STORE (the
// point-in-time view returns this unchanged, generated_at included, so the viewer
// sees exactly what was captured) and (b) the sha256 hex of the CANONICAL form to
// store as content_hash. Storing the verbatim envelope but hashing the canonical
// form keeps the stored view faithful while making the hash stable.

// canonicalEnvelopeJSON marshals the envelope to its verbatim storage JSON and
// computes the stable content hash over a canonicalised copy (generated_at
// stripped). It returns the storage bytes and the hex hash.
func canonicalEnvelopeJSON(env ReportEnvelope) (storage json.RawMessage, contentHash string, err error) {
	storage, err = json.Marshal(env)
	if err != nil {
		return nil, "", err
	}
	contentHash, err = hashCanonicalEnvelope(storage)
	if err != nil {
		return nil, "", err
	}
	return storage, contentHash, nil
}

// hashCanonicalEnvelope computes the sha256 hex over a canonicalised copy of the
// envelope JSON: the volatile metadata.generated_at is removed, then the value
// is re-marshalled via encoding/json (which emits object keys in sorted order),
// so two renders of identical data hash identically. Operating on the raw JSON
// (not the typed struct) means the canonicalisation is robust to any chart_data
// shape the report carries.
//
// Numbers are decoded with UseNumber() so a JSON number is preserved as its exact
// literal text (json.Number) rather than round-tripped through float64. The four
// currently snapshot-able builders emit all money/litre figures as decimal STRINGS
// and never set numeric chart_data, so this is belt-and-suspenders today; but it
// makes the hash deterministic by construction for any FUTURE report that carries a
// numeric field (e.g. a large integer > 2^53 or a high-precision decimal), where a
// float64 round-trip could otherwise perturb the canonical bytes and the hash.
func hashCanonicalEnvelope(raw json.RawMessage) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var generic map[string]any
	if err := dec.Decode(&generic); err != nil {
		return "", err
	}
	// Strip the per-render timestamp so the hash is stable across captures of the
	// same data. The stored envelope (above) keeps it for an honest point-in-time
	// view; only the HASH input is canonicalised.
	if md, ok := generic["metadata"].(map[string]any); ok {
		delete(md, "generated_at")
	}
	canon, err := json.Marshal(generic)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:]), nil
}
