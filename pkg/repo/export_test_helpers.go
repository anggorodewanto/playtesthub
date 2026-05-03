package repo

import (
	"time"

	"github.com/google/uuid"
)

// EncodeApplicantPageTokenForTest exposes the cursor encoder so the
// service-package fake ApplicantStore can round-trip tokens without
// duplicating the JSON+base64 format. Production code never calls this
// directly — the encoder runs inside ListPaged.
func EncodeApplicantPageTokenForTest(createdAt time.Time, id uuid.UUID) string {
	return encodeApplicantPageToken(applicantCursor{CreatedAt: createdAt, ID: id})
}

// DecodeApplicantPageTokenForTest exposes the decoder for the same
// reason. Returns ErrInvalidPageToken on malformed input. The struct
// shape is the cursor's wire shape; tests treat it opaquely.
func DecodeApplicantPageTokenForTest(token string) (*ApplicantCursor, error) {
	c, err := decodeApplicantPageToken(token)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, nil
	}
	return &ApplicantCursor{CreatedAt: c.CreatedAt, ID: c.ID}, nil
}

// ApplicantCursor is the externally-visible shape of the opaque cursor.
type ApplicantCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}
