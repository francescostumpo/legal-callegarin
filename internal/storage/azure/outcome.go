package azure

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
	"github.com/francescostumpo/legal-callegarin/internal/auth"
	"github.com/francescostumpo/legal-callegarin/internal/contacts"
)

func mutationOutcomeMayBeUnknown(err error) bool {
	return errors.Is(err, ErrTransient) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func unknownCommit(domain, cause error) error {
	return fmt.Errorf("%w: %w", domain, cause)
}

func unknownCommitForContext(ctx context.Context, domain, cause error) error {
	if err := ctx.Err(); err != nil && !errors.Is(cause, err) {
		return fmt.Errorf("%w: %w: %w", domain, cause, err)
	}
	return unknownCommit(domain, cause)
}

func sameArticleState(left, right articles.Article) bool {
	leftValue, leftErr := marshalArticleEntity(left)
	rightValue, rightErr := marshalArticleEntity(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftValue, rightValue)
}

func sameContactState(left, right contacts.Contact) bool {
	leftValue, leftErr := marshalContactEntity(left)
	rightValue, rightErr := marshalContactEntity(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftValue, rightValue)
}

func sameSessionState(left, right auth.Session) bool {
	leftValue, leftErr := marshalSessionEntity(left)
	rightValue, rightErr := marshalSessionEntity(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftValue, rightValue)
}
