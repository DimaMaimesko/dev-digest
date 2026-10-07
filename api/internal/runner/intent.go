package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/DimaMaimesko/dev-digest/api/internal/git"
	"github.com/DimaMaimesko/dev-digest/api/internal/github"
	"github.com/DimaMaimesko/dev-digest/api/internal/intent"
	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
	"github.com/DimaMaimesko/dev-digest/api/internal/review"
)

// intentDefaultProvider and intentDefaultModel are used when the workspace
// has no saved feature_models.review_intent choice: it mirrors FEATURE_MODELS
// in client/src/vendor/shared/contracts/platform.ts.
const (
	intentDefaultProvider = "anthropic"
	intentDefaultModel    = "haiku"

	defaultIntentTimeout = 60 * time.Second
	// maxBlobBytes caps how much of one document's blob ReadBlob loads, well
	// above intent.Gather's own rune cap, just so an enormous or binary file
	// can't be read in full before Gather decides what to do with it.
	maxBlobBytes = 1 << 20
)

// intentResult is a pull request's intent, as of one request to review it:
// the outcome of intentFor, carried into every job's prompt and trace. Its
// zero value (status == "") means no intent system is wired, as in tests
// that leave Config.IntentLLM nil.
type intentResult struct {
	intent   *intent.Intent // nil on "failed", or when no intent system is wired
	status   string         // "derived", "reused" or "failed"
	reason   string         // set on "failed"
	provider string
	model    string
	usage    review.Usage // the intent model's usage; zero when reused, failed before a call, or not this job's trigger run
}

// intentFor resolves pull's intent once per request: it reuses the stored
// one when its fingerprint still matches the PR's current head commit,
// title and body (AC-18), otherwise it derives a new one through Gather and
// Derive and stores it. ctx is the trigger run's context: a cancel of that
// run stops derivation, and the other runs review without intent.
//
// It logs exactly one line through log (the shared log of every run of the
// request), so every run's live log and trace show the same outcome.
func (r *Runner) intentFor(ctx context.Context, pull postgres.PullRequest, repo postgres.Repo, log *runLog) intentResult {
	if r.intentLLM == nil {
		return intentResult{} // only in tests that don't wire an intent model
	}

	fingerprint := intent.Fingerprint(pull.HeadSha, pull.Title, prBody(pull))
	if stored, err := r.q.GetPullIntent(ctx, pull.ID); err == nil && stored.Fingerprint != nil && *stored.Fingerprint == fingerprint {
		if reused, err := intentFromRow(stored); err == nil {
			log.info("intent: reused")
			return intentResult{intent: &reused, status: "reused", provider: derefStr(stored.Provider), model: derefStr(stored.Model)}
		}
	}

	provider, model := r.intentModelChoice(ctx, pull.WorkspaceID)
	llm, err := r.intentLLM(provider, model)
	if err != nil {
		log.info("intent: failed — " + err.Error() + "; reviewing without intent")
		return intentResult{status: "failed", reason: err.Error(), provider: provider, model: model}
	}

	timeout := r.intentTimeout
	if timeout <= 0 {
		timeout = defaultIntentTimeout
	}
	dctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	token := r.githubTokenValue()
	files := r.intentFileReader(repo, pull.HeadSha, token)
	var issues intent.IssueReader
	if token != "" {
		issues = &githubIssueReader{client: github.New(r.githubAPI, token), owner: repo.Owner, name: repo.Name}
	}
	sources := intent.Gather(dctx, r.gatherInput(dctx, pull, repo), files, issues)

	derived, usage, err := intent.Derive(dctx, llm, model, sources)
	if err != nil {
		reason := intentFailureReason(err, timeout)
		log.info("intent: failed — " + reason + "; reviewing without intent")
		return intentResult{status: "failed", reason: reason, provider: provider, model: model, usage: usage}
	}

	// Detached from ctx, like finishFailed: a cancel from now on must not
	// lose an answer that was already paid for.
	saveCtx, saveCancel := context.WithTimeout(context.WithoutCancel(r.ctx), 30*time.Second)
	defer saveCancel()
	if err := r.q.UpsertPullIntent(saveCtx, upsertIntentParams(pull, derived, fingerprint, provider, model, usage)); err != nil {
		r.log.Error("intent not saved", "pr", pull.ID, "err", err)
	}

	log.info(fmt.Sprintf("intent: derived (confidence %s, %d source(s), %d unresolved)",
		derived.Confidence, len(derived.Sources), len(derived.Unresolved)))
	return intentResult{intent: &derived, status: "derived", provider: provider, model: model, usage: usage}
}

// intentFailureReason turns an error from intent.Derive, or from resolving
// its model, into one of the fixed reasons the live log and trace show.
func intentFailureReason(err error, timeout time.Duration) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "run cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timed out after " + formatTimeout(timeout)
	case errors.Is(err, intent.ErrInvalidIntent):
		return "model gave no valid intent in 2 attempts"
	default:
		return err.Error()
	}
}

// formatTimeout renders d the way the spec's examples do: whole seconds as
// "Ns" (not Go's "1m0s" for a whole minute), anything else as Go's own
// Duration.String.
func formatTimeout(d time.Duration) string {
	if d > 0 && d%time.Second == 0 {
		return fmt.Sprintf("%ds", int64(d/time.Second))
	}
	return d.String()
}

// intentModelChoice reads the workspace's saved feature_models.review_intent
// choice, falling back to the Go default when none is saved, or it's
// malformed.
func (r *Runner) intentModelChoice(ctx context.Context, workspace uuid.UUID) (provider, model string) {
	provider, model = intentDefaultProvider, intentDefaultModel
	rows, err := r.q.ListSettings(ctx, workspace)
	if err != nil {
		return provider, model
	}
	for _, row := range rows {
		if row.Key != "feature_models" {
			continue
		}
		var choices map[string]struct {
			Provider string `json:"provider"`
			Model    string `json:"model"`
		}
		if json.Unmarshal(row.Value, &choices) != nil {
			continue
		}
		// Rows are ordered workspace-wide first, then per-user: a later,
		// matching row wins, the same fold settings() does.
		if c, ok := choices["review_intent"]; ok && c.Provider != "" && c.Model != "" {
			provider, model = c.Provider, c.Model
		}
	}
	return provider, model
}

// gatherInput reads what intent.Gather needs about pull beyond its title and
// description: its commit subjects and changed files.
func (r *Runner) gatherInput(ctx context.Context, pull postgres.PullRequest, repo postgres.Repo) intent.GatherInput {
	in := intent.GatherInput{
		Title: pull.Title, Description: prBody(pull), Owner: repo.Owner, Name: repo.Name,
		Self: int(pull.Number), Branch: pull.Branch,
	}
	if commits, err := r.q.ListPullCommits(ctx, pull.ID); err == nil {
		for _, c := range commits {
			in.Commits = append(in.Commits, firstLine(c.Message))
		}
	}
	if files, err := r.q.ListPullFiles(ctx, pull.ID); err == nil {
		for _, f := range files {
			in.Files = append(in.Files, intent.FileStat{Path: f.Path, Additions: int(f.Additions), Deletions: int(f.Deletions)})
		}
	}
	return in
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func prBody(pull postgres.PullRequest) string {
	if pull.Body == nil {
		return ""
	}
	return *pull.Body
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// githubTokenValue reads the GitHub token to read linked issues and fetch
// missing commits with, or "" when none is configured.
func (r *Runner) githubTokenValue() string {
	if r.githubToken == nil {
		return ""
	}
	token, err := r.githubToken()
	if err != nil {
		return ""
	}
	return token
}

// intentFileReader returns the FileReader intent.Gather reads documents
// through: the PR's repository clone, at its head commit.
func (r *Runner) intentFileReader(repo postgres.Repo, headSHA, token string) intent.FileReader {
	return &cloneFileReader{dir: filepath.Join(r.cloneDir, repo.Owner, repo.Name), commit: headSHA, token: token, log: r.log}
}

// cloneFileReader reads a pull request's documents from its repository's
// clone, at its head commit. A clone is usually shallow, so the head commit
// is often missing; ensure fetches it once, on the first Read, and every
// later Read reuses that attempt's outcome.
//
// FetchCommit's "git fetch --depth 1" can collide with the repo-intel
// indexer's Sync on the same clone (a shallow.lock file); that race is a
// known risk this package can't fix on its own.
type cloneFileReader struct {
	dir, commit, token string
	log                *slog.Logger

	ensured bool
	err     error
}

func (c *cloneFileReader) Read(ctx context.Context, path string) (data []byte, symlink bool, err error) {
	if err := c.ensure(ctx); err != nil {
		return nil, false, err
	}
	data, mode, err := git.ReadBlob(ctx, c.dir, c.commit, path, maxBlobBytes)
	if errors.Is(err, git.ErrNotFound) {
		return nil, false, intent.ErrNotFound
	}
	if err != nil {
		return nil, false, err
	}
	return data, mode == "120000", nil
}

func (c *cloneFileReader) ensure(ctx context.Context) error {
	if c.ensured {
		return c.err
	}
	c.ensured = true
	if _, statErr := os.Stat(filepath.Join(c.dir, ".git")); statErr != nil {
		c.err = intent.ErrNoClone
		return c.err
	}
	if git.HasCommit(ctx, c.dir, c.commit) {
		return nil
	}
	if fetchErr := git.FetchCommit(ctx, c.dir, c.commit, c.token); fetchErr != nil {
		if ctx.Err() != nil {
			c.err = ctx.Err()
			return c.err
		}
		if c.log != nil {
			c.log.Warn("head commit fetch failed", "dir", c.dir, "err", fetchErr)
		}
		c.err = intent.ErrNoCommit
	}
	return c.err
}

// githubIssueReader reads a pull request's own repository's issues through
// internal/github, mapping its errors to intent's sentinel reasons.
type githubIssueReader struct {
	client      *github.Client
	owner, name string
}

func (g *githubIssueReader) Issue(ctx context.Context, n int) (title, body string, err error) {
	issue, err := g.client.Issue(ctx, g.owner, g.name, n)
	if err != nil {
		if status, ok := errors.AsType[*github.StatusError](err); ok {
			switch {
			case status.Code == http.StatusNotFound:
				return "", "", intent.ErrIssueNotFound
			case status.Code == http.StatusTooManyRequests:
				return "", "", intent.ErrRateLimited
			case status.Code == http.StatusForbidden && strings.Contains(strings.ToLower(status.Message), "rate limit"):
				return "", "", intent.ErrRateLimited
			}
		}
		return "", "", err
	}
	return issue.Title, issue.Body, nil
}

// sourceJSON and unresolvedJSON are IntentSource and IntentUnresolved in
// client/src/vendor/shared/contracts/brief.ts: lowercase JSON names,
// independent of internal/intent's own (capitalized) Go field names.
type sourceJSON struct {
	Kind      string  `json:"kind"`
	Label     string  `json:"label"`
	Ref       *string `json:"ref"`
	Truncated bool    `json:"truncated"`
}

type unresolvedJSON struct {
	Ref    string `json:"ref"`
	Reason string `json:"reason"`
}

func toSourceJSON(sources []intent.Source) []sourceJSON {
	out := make([]sourceJSON, len(sources))
	for i, s := range sources {
		out[i] = sourceJSON{Kind: string(s.Kind), Label: s.Label, Ref: orNull(s.Ref), Truncated: s.Truncated}
	}
	return out
}

func toUnresolvedJSON(items []intent.Unresolved) []unresolvedJSON {
	out := make([]unresolvedJSON, len(items))
	for i, u := range items {
		out[i] = unresolvedJSON{Ref: u.Ref, Reason: u.Reason}
	}
	return out
}

// intentFromRow turns a stored pr_intent row back into an Intent, for reuse
// (AC-18). Its jsonb columns were written in the shape toSourceJSON /
// toUnresolvedJSON / json.Marshal([]string) produce.
func intentFromRow(row postgres.PrIntent) (intent.Intent, error) {
	var inScope, outScope []string
	if err := json.Unmarshal(row.InScope, &inScope); err != nil {
		return intent.Intent{}, err
	}
	if err := json.Unmarshal(row.OutOfScope, &outScope); err != nil {
		return intent.Intent{}, err
	}
	var rawSources []sourceJSON
	if err := json.Unmarshal(row.Sources, &rawSources); err != nil {
		return intent.Intent{}, err
	}
	var rawUnresolved []unresolvedJSON
	if err := json.Unmarshal(row.Unresolved, &rawUnresolved); err != nil {
		return intent.Intent{}, err
	}

	sources := make([]intent.Source, len(rawSources))
	for i, s := range rawSources {
		sources[i] = intent.Source{Kind: intent.Kind(s.Kind), Label: s.Label, Ref: derefStr(s.Ref), Truncated: s.Truncated}
	}
	unresolved := make([]intent.Unresolved, len(rawUnresolved))
	for i, u := range rawUnresolved {
		unresolved[i] = intent.Unresolved{Ref: u.Ref, Reason: u.Reason}
	}
	return intent.Intent{
		Statement: row.Intent, InScope: inScope, OutOfScope: outScope,
		Confidence: intent.Level(row.Confidence), Sources: sources, Unresolved: unresolved,
	}, nil
}

// upsertIntentParams is UpsertPullIntent's arguments for a newly derived
// intent of pull, under provider/model, having spent usage.
func upsertIntentParams(pull postgres.PullRequest, in intent.Intent, fingerprint, provider, model string, usage review.Usage) postgres.UpsertPullIntentParams {
	return postgres.UpsertPullIntentParams{
		PrID:        pull.ID,
		Intent:      in.Statement,
		InScope:     mustJSON(in.InScope),
		OutOfScope:  mustJSON(in.OutOfScope),
		Confidence:  string(in.Confidence),
		Sources:     mustJSON(toSourceJSON(in.Sources)),
		Unresolved:  mustJSON(toUnresolvedJSON(in.Unresolved)),
		HeadSha:     new(pull.HeadSha),
		Fingerprint: &fingerprint,
		Provider:    &provider,
		Model:       &model,
		TokensIn:    new(int32(usage.TokensIn)),
		TokensOut:   new(int32(usage.TokensOut)),
		CostUsd:     usage.CostUSD,
	}
}
