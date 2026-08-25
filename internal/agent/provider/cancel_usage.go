package provider

import (
	"context"
	"errors"
)

// CancelUsage captures token usage observed before a generation was cancelled.
type CancelUsage struct {
	InputTokens  int64
	OutputTokens int64
	// Available indicates whether provider-side usage was actually observed.
	Available bool
	// Source indicates where usage came from (for auditing/debug metadata).
	Source string
}

type cancelUsageError struct {
	cause error
	usage CancelUsage
}

func (e *cancelUsageError) Error() string {
	if e == nil || e.cause == nil {
		return context.Canceled.Error()
	}
	return e.cause.Error()
}

func (e *cancelUsageError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// WrapCanceledWithUsage attaches cancel-time usage details to a cancellation error.
// Non-cancellation errors are returned unchanged.
func WrapCanceledWithUsage(err error, usage CancelUsage) error {
	if err == nil {
		return nil
	}
	if !errors.Is(err, context.Canceled) {
		return err
	}
	return &cancelUsageError{
		cause: err,
		usage: usage,
	}
}

// ExtractCancelUsage returns cancel-time usage details when present.
func ExtractCancelUsage(err error) (CancelUsage, bool) {
	if err == nil {
		return CancelUsage{}, false
	}
	var wrapped *cancelUsageError
	if errors.As(err, &wrapped) && wrapped != nil {
		return wrapped.usage, true
	}
	return CancelUsage{}, false
}
