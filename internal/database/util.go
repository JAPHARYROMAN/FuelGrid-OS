package database

import "github.com/google/uuid"

// UUIDStrings renders a slice of UUIDs as their canonical strings, returning
// nil for an empty input. It's the safe way to pass a uuid set as a query
// parameter for `= ANY($n::uuid[])` filters: nil encodes to SQL NULL (so a
// `$n::uuid[] IS NULL` guard disables the filter), and []string encodes to
// text[] without relying on pgx's uuid-array codec.
func UUIDStrings(ids []uuid.UUID) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}
