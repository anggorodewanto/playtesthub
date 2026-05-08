package repo

import (
	"encoding/base64"
	"encoding/json"

	"github.com/google/uuid"
)

func encodePageCursor[T any](c T) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

// decodePageCursor reverses encodePageCursor. An empty token yields
// (nil, nil) so callers can pass it straight through. idOf extracts the
// uuid identity field; uuid.Nil after a successful unmarshal indicates
// a forged or zero-valued cursor and is rejected with invalid.
func decodePageCursor[T any](token string, idOf func(*T) uuid.UUID, invalid error) (*T, error) {
	if token == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, invalid
	}
	var c T
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, invalid
	}
	if idOf(&c) == uuid.Nil {
		return nil, invalid
	}
	return &c, nil
}
