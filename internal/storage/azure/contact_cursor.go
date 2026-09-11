package azure

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

type contactPageCursor struct {
	CreatedAt        time.Time `json:"createdAt"`
	ID               string    `json:"id"`
	NextPartitionKey string    `json:"nextPartitionKey"`
	NextRowKey       string    `json:"nextRowKey"`
}

func encodeContactPageCursor(boundary contactPageCursor, next tableContinuation) string {
	boundary.CreatedAt = boundary.CreatedAt.UTC()
	boundary.NextPartitionKey = next.PartitionKey
	boundary.NextRowKey = next.RowKey
	encoded, _ := json.Marshal(boundary)
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func decodeContactPageCursor(value string) (contactPageCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || base64.RawURLEncoding.EncodeToString(decoded) != value {
		return contactPageCursor{}, errors.New("contact cursor encoding is invalid")
	}
	decoder := json.NewDecoder(strings.NewReader(string(decoded)))
	decoder.DisallowUnknownFields()
	var cursor contactPageCursor
	if err := decoder.Decode(&cursor); err != nil {
		return contactPageCursor{}, err
	}
	if cursor.CreatedAt.IsZero() || !safeStorageSegment(cursor.ID) || cursor.NextPartitionKey == "" && cursor.NextRowKey == "" {
		return contactPageCursor{}, errors.New("contact cursor is incomplete")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return contactPageCursor{}, errors.New("contact cursor has trailing data")
	}
	return cursor, nil
}
