package azure

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

const maxArticlePageCursorLength = 512

type articlePageCursor struct {
	Page      int       `json:"page"`
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

func encodeArticlePageCursor(cursor articlePageCursor) string {
	cursor.CreatedAt = cursor.CreatedAt.UTC()
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}
func decodeArticlePageCursor(value string) (articlePageCursor, error) {
	if len(value) > maxArticlePageCursorLength {
		return articlePageCursor{}, errors.New("cursor is too long")
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || base64.RawURLEncoding.EncodeToString(raw) != value {
		return articlePageCursor{}, errors.New("invalid cursor")
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	var c articlePageCursor
	if d.Decode(&c) != nil || c.Page < 1 || c.CreatedAt.IsZero() || !safeStorageSegment(c.ID) {
		return articlePageCursor{}, errors.New("incomplete cursor")
	}
	if err = d.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return articlePageCursor{}, errors.New("trailing cursor")
	}
	return c, nil
}
