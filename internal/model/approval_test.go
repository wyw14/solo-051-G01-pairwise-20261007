package model

import (
	"testing"
	"time"
)

func TestValidateRunRequiresRevision(t *testing.T) {
	run := Run{ID: NewID(), Batch: "batch", RecipeID: NewID(), RecipeRevision: 0, State: Loading}
	if err := ValidateRun(run); err == nil {
		t.Fatal("run without a bound immutable revision must be invalid")
	}
	run.RecipeRevision = 1
	if err := ValidateRun(run); err != nil {
		t.Fatalf("run with a bound revision should be valid: %v", err)
	}
}

func TestApprovalEvidenceValidation(t *testing.T) {
	evidence := ApprovalEvidence{RevisionID: NewID(), Role: ApprovalProcess, Reviewer: "alice", ContentDigest: "abc"}
	if err := evidence.Validate(); err == nil {
		t.Fatal("evidence without a timestamp must be invalid")
	}
	evidence.At = time.Now().UTC()
	if err := evidence.Validate(); err != nil {
		t.Fatalf("complete evidence should be valid: %v", err)
	}
	evidence.Role = "manager"
	if err := evidence.Validate(); err == nil {
		t.Fatal("evidence with an unknown role must be invalid")
	}
	if !ApprovalQuality.Valid() || ApprovalRole("").Valid() {
		t.Fatal("approval role validity check is wrong")
	}
}

func TestRunTerminalStates(t *testing.T) {
	if !Released.IsTerminal() || !Failed.IsTerminal() {
		t.Fatal("released and failed runs must be terminal")
	}
	if Coating.IsTerminal() {
		t.Fatal("coating runs must not be terminal")
	}
}
