package azure

import (
	"errors"
)

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
