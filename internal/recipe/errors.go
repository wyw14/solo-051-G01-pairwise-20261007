package recipe

import "errors"

// Sentinel errors used to classify governance failures at the API edge.
// All operational errors wrap one of these so callers can map them to
// stable HTTP status codes without parsing messages.
var (
	// ErrCompile: a draft fails the missing-layer / illegal-order contract.
	ErrCompile = errors.New("compile validation failed")
	// ErrReviewNotFound: no review record exists for the revision.
	ErrReviewNotFound = errors.New("review record not found")
	// ErrNoActiveRelease: the recipe has no published release runs can bind.
	ErrNoActiveRelease = errors.New("no active release")
	// ErrActiveRun: a started, non-terminal run blocks the release change.
	ErrActiveRun = errors.New("in-flight run blocks release change")
	// ErrApproval: required approval evidence is missing or invalid.
	ErrApproval = errors.New("approval evidence insufficient")
	// ErrInvalidState: the release status forbids the requested transition.
	ErrInvalidState = errors.New("invalid release state")
	// ErrAlreadyExists: the revision is already under governance.
	ErrAlreadyExists = errors.New("revision already governed")
)
