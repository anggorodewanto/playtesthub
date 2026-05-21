// Package agsid formats user / actor UUIDs into the dashless 32-char hex
// form that AccelByte Gaming Services uses on the wire (JWT `sub` claim,
// IAM API responses, Platform API references). Our Postgres `user_id`
// columns are typed `UUID`, so pgx returns canonical dashed form on read
// — every outgoing surface (gRPC responses, audit JSONB, structured
// logs) routes through Format so admins see the same id shape the AGS
// portal shows.
package agsid

import (
	"strings"

	"github.com/google/uuid"
)

// Format returns u in dashless 32-char lowercase hex. The zero UUID
// returns 32 zeros — callers that need to distinguish "unset" from a
// real id should branch on uuid.Nil before calling.
func Format(u uuid.UUID) string {
	return strings.ReplaceAll(u.String(), "-", "")
}

// FormatPtr returns Format(*u) for non-nil pointers, or "" when u is
// nil. Used for nullable proto string fields (e.g. Code.reserved_by)
// where a missing id must serialize as the empty string rather than 32
// zeros.
func FormatPtr(u *uuid.UUID) string {
	if u == nil {
		return ""
	}
	return Format(*u)
}
