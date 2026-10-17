package onboarding

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
	"github.com/DimaMaimesko/dev-digest/api/internal/tour"
)

// SectionStatus is one section's status within a Generation, and the shape
// stored in onboarding_runs.sections: the TourSectionKind/TourSectionStatus
// pair from the OnboardingGeneration contract
// (client/src/vendor/shared/contracts/onboarding.ts). httpapi embeds
// SectionStatus directly in its own wire type, so these tags are what the
// client actually reads: keep them in step with the contract.
type SectionStatus struct {
	Kind   tour.Kind   `json:"kind"`
	Status tour.Status `json:"status"`
	// Reason is set only once Status is "failed".
	Reason *string `json:"reason"`
}

// Generation is one onboarding_runs row, matching the OnboardingGeneration
// contract field for field. httpapi re-maps every field into its own
// generationJSON (jsTime-formatted timestamps, string ids), so Generation
// itself is never marshalled: it carries no json tags.
type Generation struct {
	ID         uuid.UUID
	RepoID     uuid.UUID
	Status     string // "queued", "running", "done" or "failed"
	Error      *string
	CommitSha  string
	CreatedAt  time.Time
	FinishedAt *time.Time
	Sections   []SectionStatus
}

// TourSection is one section of a stored tour, matching the TourSection
// contract field for field. Content is nil (renders as JSON null) when
// Status is "failed"; otherwise it is one of internal/tour's wire content
// types for Kind (tour.ArchitectureContent and so on), which already carry
// json tags matching the contract. httpapi embeds TourSection directly in
// its own wire type (tourJSON.Sections), so these tags are what the client
// actually reads.
type TourSection struct {
	Kind        tour.Kind `json:"kind"`
	Status      string    `json:"status"` // "done" or "failed"
	Reason      *string   `json:"reason"`
	RemovedRefs int       `json:"removed_refs"`
	Content     any       `json:"content"`
}

// Tour is a repository's stored onboarding tour, matching the OnboardingTour
// contract field for field. httpapi re-maps every field into its own
// tourJSON (a jsTime-formatted generated_at), so Tour itself is never
// marshalled: it carries no json tags.
type Tour struct {
	RepoID         uuid.UUID
	CommitSha      string
	FilesIndexed   int32
	IndexStatus    string // "full" or "partial"
	CodeIndexEmpty bool
	GeneratedAt    time.Time
	Provider       string
	Model          string
	CostUsd        *float64
	Sections       []TourSection
}

// IndexReadiness is a repository's index readiness, matching the
// RepoIndexReadiness contract field for field. httpapi embeds IndexReadiness
// directly in its own wire type (onboardingTourResponseJSON.Index), so
// these tags are what the client actually reads.
type IndexReadiness struct {
	State          string  `json:"state"` // "ready", "indexing" or "not_indexed"
	CommitSha      *string `json:"commit_sha"`
	FilesIndexed   int32   `json:"files_indexed"`
	Partial        bool    `json:"partial"`
	CodeIndexEmpty bool    `json:"code_index_empty"`
}

// View is Get's answer, matching the OnboardingTourResponse contract field
// for field. httpapi rebuilds it field by field as its own
// onboardingTourResponseJSON, so View itself is never marshalled: it
// carries no json tags.
type View struct {
	Index      IndexReadiness
	Tour       *Tour
	Generation *Generation
}

// initialSections is a freshly inserted run's sections: every kind pending,
// in tour.Kinds' fixed order.
func initialSections() []SectionStatus {
	out := make([]SectionStatus, len(tour.Kinds))
	for i, k := range tour.Kinds {
		out[i] = SectionStatus{Kind: k, Status: tour.StatusPending}
	}
	return out
}

// kindIndex returns kind's position in tour.Kinds, for SetOnboardingSection's
// jsonb path.
func kindIndex(kind tour.Kind) int {
	for i, k := range tour.Kinds {
		if k == kind {
			return i
		}
	}
	return -1
}

// generationFromRow builds a Generation from an onboarding_runs row.
func generationFromRow(run postgres.OnboardingRun) (Generation, error) {
	var sections []SectionStatus
	if err := json.Unmarshal(run.Sections, &sections); err != nil {
		return Generation{}, err
	}
	return Generation{
		ID: run.ID, RepoID: run.RepoID, Status: run.Status, Error: run.Error,
		CommitSha: run.CommitSha, CreatedAt: run.CreatedAt, FinishedAt: run.FinishedAt,
		Sections: sections,
	}, nil
}

// tourFromRow builds a Tour from an onboarding row.
func tourFromRow(row postgres.Onboarding) (Tour, error) {
	var sections []TourSection
	if err := json.Unmarshal(row.Json, &sections); err != nil {
		return Tour{}, err
	}
	return Tour{
		RepoID: row.RepoID, CommitSha: derefStr(row.CommitSha), FilesIndexed: row.FilesIndexed,
		IndexStatus: derefStr(row.IndexStatus), CodeIndexEmpty: row.CodeIndexEmpty,
		GeneratedAt: row.GeneratedAt, Provider: derefStr(row.Provider), Model: derefStr(row.Model),
		CostUsd: row.CostUsd, Sections: sections,
	}, nil
}

// sectionsToJSON renders Generate's result as the TourSection array stored
// in onboarding.json and returned by Get.
func sectionsToJSON(sections [5]tour.SectionResult) ([]byte, error) {
	out := make([]TourSection, len(sections))
	for i, sr := range sections {
		out[i] = TourSection{
			Kind: sr.Kind, Status: string(sr.Status), Reason: orNull(sr.Reason),
			RemovedRefs: sr.RemovedRefs, Content: sr.Content,
		}
	}
	return json.Marshal(out)
}

// orNull returns nil for an empty string, and a pointer to s otherwise: the
// "string | null" shape the contract uses for an optional reason.
func orNull(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
