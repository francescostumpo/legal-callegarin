package azure

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

type articlePageCursor struct {
	Page             int    `json:"page"`
	NextPartitionKey string `json:"nextPartitionKey"`
	NextRowKey       string `json:"nextRowKey"`
}

func encodeArticlePageCursor(cursor articlePageCursor, next tableContinuation) string {
	cursor.NextPartitionKey = next.PartitionKey
	cursor.NextRowKey = next.RowKey
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}
func decodeArticlePageCursor(value string) (articlePageCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || base64.RawURLEncoding.EncodeToString(raw) != value {
		return articlePageCursor{}, errors.New("invalid cursor")
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	var c articlePageCursor
	if d.Decode(&c) != nil || c.Page < 1 || c.NextPartitionKey == "" && c.NextRowKey == "" {
		return articlePageCursor{}, errors.New("incomplete cursor")
	}
	if err = d.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return articlePageCursor{}, errors.New("trailing cursor")
	}
	return c, nil
}
