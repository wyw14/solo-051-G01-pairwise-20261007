package recipe

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/wyw14/cry-161/internal/model"
)

// ReleaseStatus is the lifecycle state of a recipe release.
type ReleaseStatus string

const (
	// ReviewInReview: frozen revision under review, not runnable.
	ReviewInReview ReleaseStatus = "in_review"
	// ReviewRejected: review failed; the revision can never be published.
	ReviewRejected ReleaseStatus = "rejected"
	// ReleaseActive: published and the target for new runs.
	ReleaseActive ReleaseStatus = "active"
	// ReleaseSuperseded: replaced by a newer published release.
	ReleaseSuperseded ReleaseStatus = "superseded"
	// ReleaseWithdrawn: pulled from service; no new runs may bind to it.
	ReleaseWithdrawn ReleaseStatus = "withdrawn"
)

// ApprovalRole identifies a required approval track.
type ApprovalRole string

const (
	RoleProcessEngineer ApprovalRole = "process_engineer"
	RoleQuality         ApprovalRole = "quality"
)

// RequiredApprovalRoles is the minimum approval evidence every publish needs.
var RequiredApprovalRoles = []ApprovalRole{RoleProcessEngineer, RoleQuality}

// Approval is immutable review evidence attached to a release.
type Approval struct {
	Role       ApprovalRole `json:"role"`
	Reviewer   string       `json:"reviewer"`
	EvidenceID string       `json:"evidence_id"`
	At         time.Time    `json:"at"`
}

func (a Approval) valid() error {
	if a.Role == "" {
		return fmt.Errorf("approval role is required")
	}
	if a.Reviewer == "" {
		return fmt.Errorf("approval reviewer is required")
	}
	if a.EvidenceID == "" {
		return fmt.Errorf("approval evidence is insufficient: evidence id required")
	}
	if a.At.IsZero() {
		return fmt.Errorf("approval timestamp is required")
	}
	return nil
}

// ReleaseRecord binds one frozen, immutable Revision to its governance
// lifecycle. The revision content never changes after creation; only the
// status moves through the review/publish/withdraw/rollback flow.
type ReleaseRecord struct {
	ID             model.ID       `json:"id"`
	RecipeID       model.ID       `json:"recipe_id"`
	RevisionID     model.ID       `json:"revision_id"`
	RevisionNumber int            `json:"revision_number"`
	Layers         []model.Layer  `json:"layers"`
	Status         ReleaseStatus  `json:"status"`
	Approvals      []Approval     `json:"approvals"`
	OpenedAt       time.Time      `json:"opened_at"`
	PublishedAt    time.Time      `json:"published_at,omitempty"`
	ClosedAt       time.Time      `json:"closed_at,omitempty"`
	RejectReason   string         `json:"reject_reason,omitempty"`
	History        []ReleaseEvent `json:"history"`
}

// ReleaseEvent is an append-only governance audit entry.
type ReleaseEvent struct {
	Kind   string    `json:"kind"`
	Actor  string    `json:"actor"`
	Detail string    `json:"detail,omitempty"`
	At     time.Time `json:"at"`
}

func (r *ReleaseRecord) append(kind, actor, detail string) {
	r.History = append(r.History, ReleaseEvent{Kind: kind, Actor: actor, Detail: detail, At: time.Now().UTC()})
}

// HasApproval reports whether every required role is covered, each by a
// distinct reviewer.
func (r *ReleaseRecord) HasApproval() error {
	byRole := map[ApprovalRole]Approval{}
	reviewers := map[string]bool{}
	for _, approval := range r.Approvals {
		if err := approval.valid(); err != nil {
			return err
		}
		if reviewers[approval.Reviewer] {
			return fmt.Errorf("approval evidence is insufficient: reviewer %q covers multiple roles", approval.Reviewer)
		}
		reviewers[approval.Reviewer] = true
		byRole[approval.Role] = approval
	}
	for _, role := range RequiredApprovalRoles {
		if _, ok := byRole[role]; !ok {
			return fmt.Errorf("approval evidence is insufficient: missing %s approval", role)
		}
	}
	return nil
}

// BoundRelease is the immutable snapshot a run pins itself to. Runs keep
// resolving layers through this snapshot, so later publishes or a
// withdrawal can never change an in-flight run.
type BoundRelease struct {
	RecipeID       model.ID
	RevisionID     model.ID
	RevisionNumber int
	Layers         []model.Layer
	ReleaseID      model.ID
}

// ActiveRunGuard reports whether a recipe currently has a started run that
// must keep its immutable binding. Released and failed runs have left the
// coating process and do not block governance changes.
type ActiveRunGuard interface {
	HasActiveRun(recipeID model.ID) bool
}

// ReleaseRegistry stores the governance records and performs all state
// transitions under a single lock.
type ReleaseRegistry struct {
	mu sync.RWMutex
	// records preserves full history per recipe, newest last.
	records map[model.ID][]ReleaseRecord
	// active points at the currently published release per recipe.
	active map[model.ID]model.ID
	// bound maps a run to the release it started on.
	bound map[model.ID]BoundRelease
}

func NewReleaseRegistry() *ReleaseRegistry {
	return &ReleaseRegistry{
		records: map[model.ID][]ReleaseRecord{},
		active:  map[model.ID]model.ID{},
		bound:   map[model.ID]BoundRelease{},
	}
}

// OpenReview creates an in_review record for a frozen revision.
func (g *ReleaseRegistry) OpenReview(rev Revision, actor string) (ReleaseRecord, error) {
	if rev.ID == "" || rev.RecipeID == "" {
		return ReleaseRecord{}, fmt.Errorf("%w: revision identity is required", ErrInvalidState)
	}
	if err := ValidateCompiled(rev.Layers); err != nil {
		return ReleaseRecord{}, fmt.Errorf("%w: revision %d: %v", ErrCompile, rev.Number, err)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, existing := range g.records[rev.RecipeID] {
		if existing.RevisionID == rev.ID {
			return ReleaseRecord{}, fmt.Errorf("%w: revision %d", ErrAlreadyExists, rev.Number)
		}
	}
	record := ReleaseRecord{
		ID:             model.NewID(),
		RecipeID:       rev.RecipeID,
		RevisionID:     rev.ID,
		RevisionNumber: rev.Number,
		Layers:         append([]model.Layer(nil), rev.Layers...),
		Status:         ReviewInReview,
		Approvals:      []Approval{},
		OpenedAt:       time.Now().UTC(),
		History:        []ReleaseEvent{},
	}
	record.append("review.opened", actor, fmt.Sprintf("revision=%d", rev.Number))
	g.records[rev.RecipeID] = append(g.records[rev.RecipeID], record)
	return cloneRecord(record), nil
}

// RecordApproval attaches review evidence to an in_review record.
func (g *ReleaseRegistry) RecordApproval(recipeID model.ID, revisionID model.ID, approval Approval) (ReleaseRecord, error) {
	if approval.At.IsZero() {
		approval.At = time.Now().UTC()
	}
	if err := approval.valid(); err != nil {
		return ReleaseRecord{}, fmt.Errorf("%w: %v", ErrApproval, err)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	record, index, err := g.findRecordLocked(recipeID, revisionID)
	if err != nil {
		return ReleaseRecord{}, err
	}
	if record.Status != ReviewInReview {
		return ReleaseRecord{}, fmt.Errorf("%w: revision %d is %s, approvals are only accepted in review", ErrInvalidState, record.RevisionNumber, record.Status)
	}
	for i, existing := range record.Approvals {
		if existing.Role == approval.Role {
			record.Approvals[i] = approval
			record.append("approval.replaced", approval.Reviewer, string(approval.Role))
			g.records[recipeID][index] = record
			return cloneRecord(record), nil
		}
	}
	record.Approvals = append(record.Approvals, approval)
	record.append("approval.recorded", approval.Reviewer, string(approval.Role))
	g.records[recipeID][index] = record
	return cloneRecord(record), nil
}

// Reject closes a review without publishing. A rejected revision is terminal.
func (g *ReleaseRegistry) Reject(recipeID model.ID, revisionID model.ID, actor, reason string) (ReleaseRecord, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	record, index, err := g.findRecordLocked(recipeID, revisionID)
	if err != nil {
		return ReleaseRecord{}, err
	}
	if record.Status != ReviewInReview {
		return ReleaseRecord{}, fmt.Errorf("%w: revision %d is %s, only in_review revisions can be rejected", ErrInvalidState, record.RevisionNumber, record.Status)
	}
	record.Status = ReviewRejected
	record.RejectReason = reason
	record.ClosedAt = time.Now().UTC()
	record.append("review.rejected", actor, reason)
	g.records[recipeID][index] = record
	return cloneRecord(record), nil
}

// Publish moves an approved revision to active. Any previously active
// release becomes superseded. It refuses when a run started on an older or
// newer release is still active: an in-flight run cannot be covered by a
// new publication.
func (g *ReleaseRegistry) Publish(recipeID, revisionID model.ID, actor string, guard ActiveRunGuard) (ReleaseRecord, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if guard != nil && guard.HasActiveRun(recipeID) {
		current := g.active[recipeID]
		return ReleaseRecord{}, fmt.Errorf("%w: recipe has a started run (current release %s); in-flight runs cannot be covered by a new release", ErrActiveRun, current)
	}
	record, index, err := g.findRecordLocked(recipeID, revisionID)
	if err != nil {
		return ReleaseRecord{}, err
	}
	if record.Status != ReviewInReview {
		return ReleaseRecord{}, fmt.Errorf("%w: revision %d is %s, only reviewed revisions can be published", ErrInvalidState, record.RevisionNumber, record.Status)
	}
	if err := record.HasApproval(); err != nil {
		return ReleaseRecord{}, fmt.Errorf("%w: %v", ErrApproval, err)
	}
	if previousID, exists := g.active[recipeID]; exists {
		for i, previous := range g.records[recipeID] {
			if previous.ID == previousID && previous.Status == ReleaseActive {
				previous.Status = ReleaseSuperseded
				previous.ClosedAt = time.Now().UTC()
				previous.append("release.superseded", actor, fmt.Sprintf("by revision=%d", record.RevisionNumber))
				g.records[recipeID][i] = previous
				break
			}
		}
	}
	record.Status = ReleaseActive
	record.PublishedAt = time.Now().UTC()
	record.append("release.published", actor, "")
	g.records[recipeID][index] = record
	g.active[recipeID] = record.ID
	return cloneRecord(record), nil
}

// Withdraw pulls the active release from service. Existing bound runs keep
// their immutable snapshot; no new run can bind while withdrawn.
func (g *ReleaseRegistry) Withdraw(recipeID model.ID, actor, reason string) (ReleaseRecord, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	activeID, ok := g.active[recipeID]
	if !ok {
		return ReleaseRecord{}, fmt.Errorf("%w: recipe %s", ErrNoActiveRelease, recipeID)
	}
	for i, record := range g.records[recipeID] {
		if record.ID == activeID {
			record.Status = ReleaseWithdrawn
			record.ClosedAt = time.Now().UTC()
			record.RejectReason = reason
			record.append("release.withdrawn", actor, reason)
			g.records[recipeID][i] = record
			delete(g.active, recipeID)
			return cloneRecord(record), nil
		}
	}
	return ReleaseRecord{}, fmt.Errorf("active release %s record missing", activeID)
}

// Rollback restores a previously superseded (or withdrawn) release to
// active without a new review round, because its frozen revision and its
// approval evidence are unchanged. The currently active release becomes
// superseded. Blocked while an in-flight run exists, same as Publish.
func (g *ReleaseRegistry) Rollback(recipeID, revisionID model.ID, actor string, guard ActiveRunGuard) (ReleaseRecord, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if guard != nil && guard.HasActiveRun(recipeID) {
		current := g.active[recipeID]
		return ReleaseRecord{}, fmt.Errorf("%w: recipe has a started run (current release %s); in-flight runs cannot be covered by a release change", ErrActiveRun, current)
	}
	record, index, err := g.findRecordLocked(recipeID, revisionID)
	if err != nil {
		return ReleaseRecord{}, err
	}
	switch record.Status {
	case ReleaseSuperseded, ReleaseWithdrawn:
	default:
		return ReleaseRecord{}, fmt.Errorf("%w: revision %d is %s, only superseded or withdrawn releases can roll back", ErrInvalidState, record.RevisionNumber, record.Status)
	}
	if err := record.HasApproval(); err != nil {
		return ReleaseRecord{}, fmt.Errorf("%w: %v", ErrApproval, err)
	}
	if activeID, exists := g.active[recipeID]; exists {
		for i, active := range g.records[recipeID] {
			if active.ID == activeID && active.Status == ReleaseActive {
				active.Status = ReleaseSuperseded
				active.ClosedAt = time.Now().UTC()
				active.append("release.superseded", actor, fmt.Sprintf("rollback to revision=%d", record.RevisionNumber))
				g.records[recipeID][i] = active
				break
			}
		}
	}
	record.Status = ReleaseActive
	if record.PublishedAt.IsZero() {
		record.PublishedAt = time.Now().UTC()
	}
	record.ClosedAt = time.Time{}
	record.RejectReason = ""
	record.append("release.rolled_back", actor, fmt.Sprintf("revision=%d", record.RevisionNumber))
	g.records[recipeID][index] = record
	g.active[recipeID] = record.ID
	return cloneRecord(record), nil
}

// Bind pins a starting run to the active release's immutable snapshot and
// records the binding. It fails when no release is active.
func (g *ReleaseRegistry) Bind(runID, recipeID model.ID) (BoundRelease, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	activeID, ok := g.active[recipeID]
	if !ok {
		return BoundRelease{}, fmt.Errorf("%w: recipe %s; runs must bind an immutable published version", ErrNoActiveRelease, recipeID)
	}
	for _, record := range g.records[recipeID] {
		if record.ID == activeID {
			if record.Status != ReleaseActive {
				return BoundRelease{}, fmt.Errorf("%w: release %s is %s", ErrInvalidState, activeID, record.Status)
			}
			bound := BoundRelease{
				RecipeID:       recipeID,
				RevisionID:     record.RevisionID,
				RevisionNumber: record.RevisionNumber,
				Layers:         append([]model.Layer(nil), record.Layers...),
				ReleaseID:      record.ID,
			}
			g.bound[runID] = bound
			return bound, nil
		}
	}
	return BoundRelease{}, fmt.Errorf("active release %s record missing", activeID)
}

// Binding returns the immutable release a run started on, if any.
func (g *ReleaseRegistry) Binding(runID model.ID) (BoundRelease, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	bound, ok := g.bound[runID]
	if ok {
		bound.Layers = append([]model.Layer(nil), bound.Layers...)
	}
	return bound, ok
}

// ActiveRelease returns the currently published record for a recipe.
func (g *ReleaseRegistry) ActiveRelease(recipeID model.ID) (ReleaseRecord, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	activeID, ok := g.active[recipeID]
	if !ok {
		return ReleaseRecord{}, false
	}
	for _, record := range g.records[recipeID] {
		if record.ID == activeID {
			return cloneRecord(record), true
		}
	}
	return ReleaseRecord{}, false
}

// List returns the full governance history of a recipe, newest first.
func (g *ReleaseRegistry) List(recipeID model.ID) []ReleaseRecord {
	g.mu.RLock()
	defer g.mu.RUnlock()
	result := append([]ReleaseRecord(nil), g.records[recipeID]...)
	sort.Slice(result, func(i, j int) bool { return result[i].RevisionNumber > result[j].RevisionNumber })
	for i := range result {
		result[i] = cloneRecord(result[i])
	}
	return result
}

func (g *ReleaseRegistry) findRecordLocked(recipeID, revisionID model.ID) (ReleaseRecord, int, error) {
	for index, record := range g.records[recipeID] {
		if record.RevisionID == revisionID {
			return record, index, nil
		}
	}
	return ReleaseRecord{}, -1, fmt.Errorf("%w: revision %s", ErrReviewNotFound, revisionID)
}

func cloneRecord(in ReleaseRecord) ReleaseRecord {
	out := in
	out.Layers = append([]model.Layer(nil), in.Layers...)
	out.Approvals = append([]Approval(nil), in.Approvals...)
	out.History = append([]ReleaseEvent(nil), in.History...)
	return out
}
