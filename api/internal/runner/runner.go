// Package runner reviews pull requests in the background. A review of a
// pull request by an agent is a run: it loads the diff and the repo-intel
// context, asks the agent's model through the review package, and saves
// the review, its findings and a trace. Each run's progress is a live log
// that the web app follows while it runs.
package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DimaMaimesko/dev-digest/api/internal/diff"
	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
	"github.com/DimaMaimesko/dev-digest/api/internal/repointel"
	"github.com/DimaMaimesko/dev-digest/api/internal/review"
)

// LLMFor returns the model client for a provider (openai, anthropic or
// openrouter), or an error saying why there is none, such as a missing key.
type LLMFor func(provider string) (review.LLM, error)

// IntentLLMFor returns the model client for the intent feature's provider
// and model (the choice saved in settings.feature_models.review_intent, or
// its default), or an error saying why there is none.
type IntentLLMFor func(provider, model string) (review.LLM, error)

// Config is what a Runner needs.
type Config struct {
	DB       *pgxpool.Pool
	LLM      LLMFor
	CloneDir string // where repositories are cloned, one directory per owner/name
	// RepoIntel turns the repo-intel context (callers, repository map, file
	// ranks) on; an agent can still turn it off for itself.
	RepoIntel bool
	Log       *slog.Logger

	// IntentLLM resolves the model that derives a pull request's intent. Nil
	// (only in tests) skips intent derivation entirely, without a log line.
	IntentLLM IntentLLMFor
	// GitHubToken returns the GitHub token to read linked issues and fetch a
	// head commit missing from the clone with, or "" for neither. Optional.
	GitHubToken func() (string, error)
	// GitHubAPI is the base URL of GitHub's REST API, for linked-issue reads.
	GitHubAPI string
	// IntentTimeout caps one intentFor call (Gather plus Derive). 0 means 60s.
	IntentTimeout time.Duration
}

// Runner starts runs and follows them.
type Runner struct {
	db        *pgxpool.Pool
	q         *postgres.Queries
	index     *repointel.Index
	llm       LLMFor
	cloneDir  string
	repoIntel bool
	log       *slog.Logger
	bus       *bus

	intentLLM     IntentLLMFor
	githubToken   func() (string, error)
	githubAPI     string
	intentTimeout time.Duration

	ctx  context.Context // ends when the runner closes
	stop context.CancelFunc
	wg   sync.WaitGroup // the runs in progress
}

// New returns a Runner. Close stops it.
func New(cfg Config) *Runner {
	ctx, stop := context.WithCancel(context.Background())
	return &Runner{
		db: cfg.DB, q: postgres.New(cfg.DB), index: repointel.New(cfg.DB), llm: cfg.LLM,
		cloneDir: cfg.CloneDir, repoIntel: cfg.RepoIntel, log: cfg.Log, bus: newBus(),
		intentLLM: cfg.IntentLLM, githubToken: cfg.GitHubToken, githubAPI: cfg.GitHubAPI, intentTimeout: cfg.IntentTimeout,
		ctx: ctx, stop: stop,
	}
}

// Close stops the runs in progress, which end as failed, and waits for
// them.
func (r *Runner) Close() {
	r.stop()
	r.wg.Wait()
}

// FailStale marks failed the runs still marked running when the server
// starts: a server that stopped left them. It returns how many there were.
func (r *Runner) FailStale(ctx context.Context) (int64, error) {
	return r.q.FailStaleRuns(ctx)
}

// Started is a run Start began.
type Started struct {
	RunID     uuid.UUID
	AgentID   uuid.UUID
	AgentName string
}

// job is an agent's run.
type job struct {
	run   uuid.UUID
	agent postgres.Agent
	ctx   context.Context // ends when the run is cancelled or the runner closes
}

// Start reviews pull with each agent, one after another in the background,
// and returns their runs at once, so their progress can be followed.
func (r *Runner) Start(ctx context.Context, pull postgres.PullRequest, repo postgres.Repo, agents []postgres.Agent) ([]Started, error) {
	var jobs []job
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		q := postgres.New(tx)
		for _, a := range agents {
			id, err := q.CreateRun(ctx, postgres.CreateRunParams{
				WorkspaceID: pull.WorkspaceID, AgentID: &a.ID, PrID: &pull.ID, Provider: &a.Provider, Model: &a.Model,
			})
			if err != nil {
				return err
			}
			jobs = append(jobs, job{run: id, agent: a})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	started := make([]Started, len(jobs))
	for i := range jobs {
		var cancel context.CancelFunc
		jobs[i].ctx, cancel = context.WithCancel(r.ctx)
		r.bus.add(jobs[i].run, cancel)
		started[i] = Started{RunID: jobs[i].run, AgentID: jobs[i].agent.ID, AgentName: jobs[i].agent.Name}
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.execute(pull, repo, jobs)
	}()
	return started, nil
}

// execute loads the diff once, then runs each job.
func (r *Runner) execute(pull postgres.PullRequest, repo postgres.Repo, jobs []job) {
	runs := make([]uuid.UUID, len(jobs))
	for i, j := range jobs {
		runs[i] = j.run
	}
	// The shared work is logged in every run's log.
	shared := r.newLog(runs...)

	var d diff.Diff
	err := shared.step("Loading PR diff", kindTool, func() (err error) {
		d, err = r.loadDiff(r.ctx, pull, repo)
		return err
	})
	if err != nil {
		msg := "Failed to load PR diff: " + err.Error()
		shared.error(msg)
		for _, j := range jobs {
			r.finishFailed(j, pull, "failed", msg, 0, review.Usage{}, intentResult{})
		}
		return
	}
	shared.info(fmt.Sprintf("Diff ready — %d changed file(s); starting %d agent run(s)", len(d.Files), len(jobs)))

	// Intent is derived at most once per request, under the first
	// not-yet-cancelled run's context (the trigger run), which also carries
	// its cost. The other runs see the same intent (or its absence) in their
	// trace, but not its usage.
	trigger, hasTrigger := firstActive(jobs)
	var ir intentResult
	if hasTrigger {
		ir = r.intentFor(trigger.ctx, pull, repo, shared)
	}

	for _, j := range jobs {
		start := time.Now()
		r.log.Info("review started", "run", j.run, "agent", j.agent.Name, "provider", j.agent.Provider, "model", j.agent.Model)
		jobIR := ir
		if !hasTrigger || j.run != trigger.run {
			jobIR.usage = review.Usage{}
		}
		spent, err := r.runOne(j, pull, repo, d, start, jobIR)
		switch {
		case err == nil:
			r.log.Info("review done", "run", j.run, "agent", j.agent.Name, "duration", time.Since(start))
		case r.bus.cancelledByUser(j.run):
			r.newLog(j.run).error("Run cancelled by user")
			r.finishFailed(j, pull, "cancelled", "Cancelled by user", time.Since(start), spent, jobIR)
			r.log.Info("review cancelled", "run", j.run, "agent", j.agent.Name)
		default:
			if r.ctx.Err() != nil {
				err = errors.New("the server stopped during the run")
			}
			r.newLog(j.run).error("Run failed: " + err.Error())
			r.finishFailed(j, pull, "failed", err.Error(), time.Since(start), spent, jobIR)
			r.log.Error("review failed", "run", j.run, "agent", j.agent.Name, "err", err)
		}
	}
}

// firstActive returns the first job whose context isn't already done, and
// whether there is one: a request whose runs were all cancelled before
// execute got to them derives no intent.
func firstActive(jobs []job) (job, bool) {
	for _, j := range jobs {
		if j.ctx.Err() == nil {
			return j, true
		}
	}
	return job{}, false
}

// runOne reviews the diff with one agent and saves the outcome. It returns
// what the model calls took, even when it fails: ir.usage (the trigger run's
// intent call, zero for the others) plus the review's own usage.
func (r *Runner) runOne(j job, pull postgres.PullRequest, repo postgres.Repo, d diff.Diff, start time.Time, ir intentResult) (review.Usage, error) {
	ctx := j.ctx
	a := j.agent
	log := r.newLog(j.run)
	log.info(fmt.Sprintf(`Starting review with agent "%s" (%s/%s)`, a.Name, a.Provider, a.Model))
	if err := ctx.Err(); err != nil {
		return ir.usage, err // cancelled before its turn; ir.usage is the intent cost already paid, when this is the trigger run
	}

	var llm review.LLM
	err := log.step(fmt.Sprintf("Resolving %s provider", a.Provider), kindTool, func() (err error) {
		llm, err = r.llm(a.Provider)
		return err
	})
	if err != nil {
		return ir.usage, err
	}

	prompt := review.Prompt{System: a.SystemPrompt, Task: taskLine(pull)}
	if pull.Body != nil {
		prompt.PRDescription = *pull.Body
	}
	if ir.intent != nil {
		prompt.Intent = ir.intent.PromptText()
	}
	if prompt.Skills, err = r.skills(ctx, a.ID, log); err != nil {
		return ir.usage, err
	}
	if !a.RepoIntel {
		log.info("Repo intel disabled for this agent — skipping context enrichment")
	} else if r.repoIntel {
		changed := make([]string, len(d.Files))
		for i, f := range d.Files {
			changed[i] = f.Path
		}
		prompt.Callers = r.callersDigest(ctx, repo, changed, log)
		prompt.RepoMap = r.repoMap(ctx, repo, log)
		prompt.Task += r.rankNote(ctx, repo, changed, log)
	}

	strategy := review.Strategy(a.Strategy)
	if strategy == "" {
		strategy = review.StrategySinglePass
	}
	res, err := review.Run(ctx, llm, review.Input{
		Model:     a.Model,
		Prompt:    prompt,
		Diff:      d,
		Strategy:  strategy,
		SessionID: fmt.Sprintf("%s/%s#%d:%s", repo.Owner, repo.Name, pull.Number, a.Name),
		OnEvent:   func(e review.Event) { log.event(string(e.Kind), e.Message) },
	})
	res.Usage = ir.usage.Plus(res.Usage)
	if err != nil {
		return res.Usage, err
	}
	return res.Usage, r.save(j, pull, res, log, time.Since(start), ir)
}

// save stores a finished review: the review and its findings, the run's
// outcome and its trace, in one transaction, if the run is still running.
// A run cancelled or deleted while its model answered keeps no review.
func (r *Runner) save(j job, pull postgres.PullRequest, res review.Result, log *runLog, took time.Duration, ir intentResult) error {
	// Not j.ctx: a cancel from now on is too late to stop the save.
	ctx := r.ctx
	a := j.agent
	findings := res.Review.Findings
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		q := postgres.New(tx)
		if _, err := q.LockRunningRun(ctx, j.run); errors.Is(err, pgx.ErrNoRows) {
			log.info("Run was cancelled before its review was saved; discarding it")
			// The answer was paid for all the same.
			return q.RecordRunUsage(ctx, usageParams(j.run, res.Usage))
		} else if err != nil {
			return err
		}

		verdict, summary, score := string(res.Review.Verdict), res.Review.Summary, int32(res.Review.Score)
		reviewID, err := q.InsertReview(ctx, postgres.InsertReviewParams{
			WorkspaceID: pull.WorkspaceID, PrID: pull.ID, AgentID: &a.ID, RunID: &j.run,
			Verdict: &verdict, Summary: &summary, Score: &score, Model: &a.Model,
		})
		if err != nil {
			return err
		}
		for _, f := range findings {
			if err := q.InsertFinding(ctx, findingParams(reviewID, f)); err != nil {
				return err
			}
		}
		log.result(fmt.Sprintf("Persisted review %s with %d finding(s)", reviewID, len(findings)))
		if err := q.MarkReviewed(ctx, postgres.MarkReviewedParams{ID: pull.ID, LastReviewedSha: &pull.HeadSha}); err != nil {
			return err
		}

		ms, tokensIn, tokensOut := int32(took.Milliseconds()), int32(res.TokensIn), int32(res.TokensOut)
		count, grounding, blockers := int32(len(findings)), res.Grounding.Summary(), int32(countBlockers(findings, a.CiFailOn))
		status := "done"
		err = q.FinishRun(ctx, postgres.FinishRunParams{
			ID: j.run, Status: &status, DurationMs: &ms, TokensIn: &tokensIn, TokensOut: &tokensOut, CostUsd: res.CostUSD,
			FindingsCount: &count, Grounding: &grounding, Score: &score, Blockers: &blockers,
		})
		if err != nil {
			return err
		}
		// The trace's log ends here: the last line below is only live.
		trace := r.successTrace(j, pull, res, took, ir)
		log.info("Run complete" + costNote(res.Usage) + "; trace persisted")
		return q.SaveTrace(ctx, postgres.SaveTraceParams{RunID: j.run, Trace: trace})
	})
	if err != nil {
		return err
	}
	r.bus.complete(j.run)
	return nil
}

// finishFailed records a run that failed or was cancelled, with what its
// model calls took and its log so far, and ends its live log.
func (r *Runner) finishFailed(j job, pull postgres.PullRequest, status, msg string, took time.Duration, spent review.Usage, ir intentResult) {
	// Saved even while the runner closes.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.ctx), 30*time.Second)
	defer cancel()
	ms, zero, grounding := int32(took.Milliseconds()), int32(0), "0/0 passed"
	tokensIn, tokensOut := int32(spent.TokensIn), int32(spent.TokensOut)
	err := r.q.FinishRun(ctx, postgres.FinishRunParams{
		ID: j.run, Status: &status, DurationMs: &ms, TokensIn: &tokensIn, TokensOut: &tokensOut, CostUsd: spent.CostUSD,
		FindingsCount: &zero, Grounding: &grounding, Error: &msg,
	})
	if err != nil {
		r.log.Error("run outcome not saved", "run", j.run, "err", err)
	}
	// FinishRun skips a run the user already marked cancelled; what its calls
	// took counts all the same.
	if err := r.q.RecordRunUsage(ctx, usageParams(j.run, spent)); err != nil {
		r.log.Error("run usage not saved", "run", j.run, "err", err)
	}
	// A deleted run has no trace to save; that error is expected.
	_ = r.q.SaveTrace(ctx, postgres.SaveTraceParams{RunID: j.run, Trace: r.failureTrace(j, pull, took, spent, ir)})
	r.bus.complete(j.run)
}

func usageParams(run uuid.UUID, u review.Usage) postgres.RecordRunUsageParams {
	in, out := int32(u.TokensIn), int32(u.TokensOut)
	return postgres.RecordRunUsageParams{ID: run, TokensIn: &in, TokensOut: &out, CostUsd: u.CostUSD}
}

// costNote says what the calls cost, for the log, when the provider said.
func costNote(u review.Usage) string {
	if u.CostUSD == nil {
		return ""
	}
	return fmt.Sprintf(" — $%.4f", *u.CostUSD)
}

// Cancel stops a run of the workspace: its work at once if it runs here,
// and its status in the database in any case, so a run a stopped server
// left can be cancelled too.
func (r *Runner) Cancel(ctx context.Context, workspace, run uuid.UUID) error {
	r.bus.publish(run, kindInfo, "Cancellation requested — stopping…")
	r.bus.cancelByUser(run)
	_, err := r.q.CancelRun(ctx, postgres.CancelRunParams{WorkspaceID: workspace, ID: run})
	r.bus.complete(run)
	return err
}

// Follow calls send with each event of a run, the earlier ones first,
// until the run ends, send fails or ctx is done. It returns false when this
// server doesn't know the run: it ran elsewhere, or ended long ago.
func (r *Runner) Follow(ctx context.Context, run uuid.UUID, send func(Event) error) (bool, error) {
	return r.bus.follow(ctx, run, send)
}

// Wait waits for the runs in progress to end. Tests use it.
func (r *Runner) Wait() { r.wg.Wait() }

func findingParams(review uuid.UUID, f review.Finding) postgres.InsertFindingParams {
	kind := string(f.Kind)
	if kind == "" {
		kind = "finding"
	}
	var suggestion *string
	if f.Suggestion != "" {
		suggestion = &f.Suggestion
	}
	return postgres.InsertFindingParams{
		ReviewID: review, File: f.File, StartLine: int32(f.StartLine), EndLine: int32(f.EndLine),
		Severity: string(f.Severity), Category: string(f.Category), Title: f.Title,
		Rationale: f.Rationale, Suggestion: suggestion, Confidence: f.Confidence, Kind: kind,
	}
}

// countBlockers counts the findings at or above the severity the agent's
// gate fails on (ci_fail_on): the run's blockers.
func countBlockers(findings []review.Finding, failOn string) int {
	rank := map[review.Severity]int{review.SeveritySuggestion: 1, review.SeverityWarning: 2, review.SeverityCritical: 3}
	least := map[string]int{"any": 1, "warning": 2, "critical": 3}[failOn]
	if failOn == "never" || least == 0 {
		least = math.MaxInt
	}
	n := 0
	for _, f := range findings {
		if rank[f.Severity] >= least {
			n++
		}
	}
	return n
}

// runLog writes events to the live log of one run, or of several for
// shared work.
type runLog struct {
	bus  *bus
	runs []uuid.UUID
}

func (r *Runner) newLog(runs ...uuid.UUID) *runLog { return &runLog{bus: r.bus, runs: runs} }

func (l *runLog) event(kind, msg string) {
	for _, run := range l.runs {
		l.bus.publish(run, kind, msg)
	}
}

func (l *runLog) info(msg string)   { l.event(kindInfo, msg) }
func (l *runLog) result(msg string) { l.event(kindResult, msg) }
func (l *runLog) error(msg string)  { l.event(kindError, msg) }

// step logs "label…" and then "label done (Nms)" around fn, or
// "label failed (Nms): error".
func (l *runLog) step(label, kind string, fn func() error) error {
	start := time.Now()
	l.event(kind, label+"…")
	if err := fn(); err != nil {
		l.error(fmt.Sprintf("%s failed (%dms): %v", label, time.Since(start).Milliseconds(), err))
		return err
	}
	l.event(kind, fmt.Sprintf("%s done (%dms)", label, time.Since(start).Milliseconds()))
	return nil
}
