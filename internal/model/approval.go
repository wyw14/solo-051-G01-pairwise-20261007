package model

import (
	"fmt"
	"time"
)

// ApprovalRole identifies the governance role that must sign a recipe
// publication. A publication requires evidence from two distinct roles.
type ApprovalRole string

const (
	ApprovalProcess ApprovalRole = "process_engineer"
	ApprovalQuality ApprovalRole = "quality_reviewer"
)

func (r ApprovalRole) Valid() bool {
	switch r {
	case ApprovalProcess, ApprovalQuality:
		return true
	default:
		return false
	}
}

// RequiredApprovalRoles are the roles whose evidence must all be present
// before a frozen revision may be published.
var RequiredApprovalRoles = []ApprovalRole{ApprovalProcess, ApprovalQuality}

// ApprovalEvidence is an immutable record that a named reviewer signed a
// specific content digest of a frozen revision on behalf of a role.
type ApprovalEvidence struct {
	RevisionID    ID           `json:"revision_id"`
	Role          ApprovalRole `json:"role"`
	Reviewer      string       `json:"reviewer"`
	ContentDigest string       `json:"content_digest"`
	Comment       string       `json:"comment,omitempty"`
	At            time.Time    `json:"at"`
}

func (e ApprovalEvidence) Validate() error {
	if e.RevisionID == "" {
		return fmt.Errorf("approval revision is required")
	}
	if !e.Role.Valid() {
		return fmt.Errorf("approval role %q is invalid", e.Role)
	}
	if e.Reviewer == "" {
		return fmt.Errorf("approval reviewer is required")
	}
	if e.ContentDigest == "" {
		return fmt.Errorf("approval content digest is required")
	}
	if e.At.IsZero() {
		return fmt.Errorf("approval time is required")
	}
	return nil
}
