package memory

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

type listCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

func encodeCursor(createdAt time.Time, id string) string {
	encoded, _ := json.Marshal(listCursor{CreatedAt: createdAt, ID: id})
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func decodeCursor(value string) (listCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return listCursor{}, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(decoded)))
	decoder.DisallowUnknownFields()
	var cursor listCursor
	if err := decoder.Decode(&cursor); err != nil {
		return listCursor{}, err
	}
	if cursor.CreatedAt.IsZero() || cursor.ID == "" {
		return listCursor{}, errors.New("cursor is incomplete")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return listCursor{}, errors.New("cursor has trailing data")
	}
	return cursor, nil
}

func boundedLimit(limit int) (int, error) {
	if limit < 0 {
		return 0, errors.New("limit cannot be negative")
	}
	if limit == 0 {
		return 25, nil
	}
	if limit > 100 {
		return 100, nil
	}
	return limit, nil
}
