package repointel

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DimaMaimesko/dev-digest/api/internal/git"
	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
)

// indexerVersion is bumped when what the indexer writes changes; a
// repository indexed by another version is indexed again in full. It is the
// TS indexer's, whose index this one writes the same way.
const indexerVersion = 2

// Budgets of an index run: past softBudget, parsing stops and the index is
// partial, before the job's 2-minute limit ends it.
const softBudget = 110 * time.Second

// incrementalLimit is how many changed files an incremental index takes;
// beyond it, a full index is faster.
const incrementalLimit = 300

// maxNameLen is the longest symbol name stored, in UTF-16 code units as
// the TS indexer counted: an index on the column limits the row size.
const maxNameLen = 255

// Indexer builds the repo-intel index of repositories: the symbols and
// references of their TypeScript and JavaScript files, the import graph,
// the files' ranks, the repository map and per-file facts.
type Indexer struct {
	db    *pgxpool.Pool
	q     *postgres.Queries
	token func() (string, error) // the GitHub token, for Resync's fetch
}

// NewIndexer returns an Indexer writing to db. token returns the GitHub
// token ("" for none).
func NewIndexer(db *pgxpool.Pool, token func() (string, error)) *Indexer {
	return &Indexer{db: db, q: postgres.New(db), token: token}
}

// Result says what an index run did.
type Result struct {
	Status       string // full, partial or degraded
	FilesIndexed int
	FilesSkipped int
	Reason       string // why it is partial or degraded, or what it did
}

// Full indexes a repository from scratch, from its clone.
func (x *Indexer) Full(ctx context.Context, repo uuid.UUID) (Result, error) {
	start := time.Now()
	basics, err := x.q.IndexBasics(ctx, repo)
	if errors.Is(err, pgx.ErrNoRows) {
		return Result{Status: "degraded", Reason: "repo_not_found"}, nil // deleted meanwhile
	}
	if err != nil {
		return Result{}, err
	}
	if basics.ClonePath == nil {
		err := x.saveState(ctx, x.q, repo, "", "degraded", 0, 0, map[string]any{
			"reason": "no_clone", "degradedReason": "no_data", "durationMs": ms(start),
		})
		return Result{Status: "degraded", Reason: "no_clone"}, err
	}
	dir := *basics.ClonePath
	head, _ := git.Head(ctx, dir) // "" when it can't be read

	files, walk := Walk(dir)
	if len(files) == 0 {
		stats := walkStats(walk)
		stats["reason"], stats["durationMs"] = "no_files", ms(start)
		err := x.saveState(ctx, x.q, repo, head, "partial", 0, walk.SkippedTooLarge, stats)
		return Result{Status: "partial", FilesSkipped: walk.SkippedTooLarge, Reason: "no_files"}, err
	}

	parsed, softBudgetReached := parseFiles(dir, files, start)
	var degraded []parseProblem
	indexed, skipped := 0, walk.SkippedTooLarge
	for _, p := range parsed {
		switch {
		case p.skipped:
		case p.problem != "":
			skipped++
			degraded = appendProblem(degraded, p)
		default:
			indexed++
		}
	}

	var edges []Edge
	var ranked int
	err = pgx.BeginFunc(ctx, x.db, func(tx pgx.Tx) error {
		q := postgres.New(tx)
		if err := q.LockRepoIndex(ctx, repo.String()); err != nil {
			return err
		}
		if err := q.DeleteSymbols(ctx, repo); err != nil {
			return err
		}
		if err := q.DeleteReferences(ctx, repo); err != nil {
			return err
		}
		if err := copyParsed(ctx, q, repo, parsed); err != nil {
			return err
		}
		if !softBudgetReached {
			edges = ImportGraph(dir, files)
			ranks := RankFiles(files, edges)
			ranked = len(ranks)
			if err := x.writeGraph(ctx, q, repo, edges, ranks, head); err != nil {
				return err
			}
			if err := q.DeleteFileFacts(ctx, repo); err != nil {
				return err
			}
			if err := insertFacts(ctx, q, repo, parsed); err != nil {
				return err
			}
		}
		status := "full"
		if softBudgetReached || len(degraded) > 0 {
			status = "partial"
		}
		stats := walkStats(walk)
		for k, v := range map[string]any{
			"filesSeen": len(files), "symbolsWritten": count(parsed, func(p parsedFile) int { return len(p.symbols) }),
			"referencesWritten": count(parsed, func(p parsedFile) int { return len(p.refs) }),
			"edgesWritten":      len(edges), "ranked": ranked,
			"factsWritten":     count(parsed, func(p parsedFile) int { return min(1, len(p.endpoints)+len(p.crons)) }),
			"hotnessAvailable": false, "softBudgetReached": softBudgetReached, "parseDegraded": problems(degraded),
			"durationMs": ms(start),
		} {
			stats[k] = v
		}
		return x.saveState(ctx, q, repo, head, status, indexed, skipped, stats)
	})
	if err != nil {
		return Result{}, err
	}
	res := Result{Status: "full", FilesIndexed: indexed, FilesSkipped: skipped}
	if softBudgetReached || len(degraded) > 0 {
		res.Status = "partial"
	}
	if softBudgetReached {
		res.Reason = "soft_budget"
	}
	return res, nil
}

// Refresh indexes the files that changed since the last index, and
// renders the graph, ranks and map again. It falls back to Full when there
// is no index yet, it was made by another indexer version, the last
// indexed commit isn't in the clone, or too many files changed.
func (x *Indexer) Refresh(ctx context.Context, repo uuid.UUID) (Result, error) {
	start := time.Now()
	basics, err := x.q.IndexBasics(ctx, repo)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && basics.ClonePath == nil {
		return Result{Status: "degraded", Reason: "no_clone"}, nil
	}
	if err != nil {
		return Result{}, err
	}
	state, err := x.q.IndexStateOf(ctx, repo)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && state.IndexerVersion != indexerVersion {
		return x.Full(ctx, repo)
	}
	if err != nil {
		return Result{}, err
	}
	dir := *basics.ClonePath
	head, err := git.Head(ctx, dir)
	if err != nil {
		return Result{Status: "degraded", Reason: "git_head_failed:" + err.Error()}, nil
	}
	if head == state.LastIndexedSha {
		err := x.q.TouchIndexState(ctx, repo)
		return Result{Status: state.Status, FilesIndexed: int(state.FilesIndexed), FilesSkipped: int(state.FilesSkipped), Reason: "sha_unchanged"}, err
	}
	all, err := git.ChangedFiles(ctx, dir, state.LastIndexedSha, head)
	if err != nil {
		return x.Full(ctx, repo) // the last indexed commit isn't in the clone
	}
	var changed []string
	for _, f := range all {
		if indexedExt[strings.ToLower(path.Ext(f))] {
			changed = append(changed, f)
		}
	}
	if len(changed) == 0 {
		err := x.q.AdvanceIndexedSha(ctx, postgres.AdvanceIndexedShaParams{RepoID: repo, LastIndexedSha: head})
		return Result{Status: state.Status, FilesIndexed: int(state.FilesIndexed), FilesSkipped: int(state.FilesSkipped), Reason: "no_supported_changes"}, err
	}
	if len(changed) > incrementalLimit {
		return x.Full(ctx, repo)
	}

	parsed, _ := parseFiles(dir, changed, time.Time{}) // no budget: a few files
	var degraded []parseProblem
	indexed, skipped := 0, 0
	for _, p := range parsed {
		switch {
		case p.problem != "":
			skipped++
			degraded = appendProblem(degraded, p)
		default:
			indexed++
		}
	}
	files, _ := Walk(dir)
	edges := ImportGraph(dir, files)
	ranks := RankFiles(files, edges)

	status := "partial"
	if len(degraded) == 0 && state.Status == "full" {
		status = "full"
	}
	err = pgx.BeginFunc(ctx, x.db, func(tx pgx.Tx) error {
		q := postgres.New(tx)
		if err := q.LockRepoIndex(ctx, repo.String()); err != nil {
			return err
		}
		if err := q.DeleteSymbolsIn(ctx, postgres.DeleteSymbolsInParams{RepoID: repo, Paths: changed}); err != nil {
			return err
		}
		if err := q.DeleteReferencesFrom(ctx, postgres.DeleteReferencesFromParams{RepoID: repo, Paths: changed}); err != nil {
			return err
		}
		if err := copyParsed(ctx, q, repo, parsed); err != nil {
			return err
		}
		if err := q.DeleteFileFactsIn(ctx, postgres.DeleteFileFactsInParams{RepoID: repo, Paths: changed}); err != nil {
			return err
		}
		if err := insertFacts(ctx, q, repo, parsed); err != nil {
			return err
		}
		if err := q.ClearDeclFiles(ctx, repo); err != nil {
			return err
		}
		if err := x.writeGraph(ctx, q, repo, edges, ranks, head); err != nil {
			return err
		}
		return x.saveState(ctx, q, repo, head, status, int(state.FilesIndexed)+indexed, int(state.FilesSkipped)+skipped, map[string]any{
			"incremental": true, "changedFiles": len(changed),
			"symbolsWritten":    count(parsed, func(p parsedFile) int { return len(p.symbols) }),
			"referencesWritten": count(parsed, func(p parsedFile) int { return len(p.refs) }),
			"edgesWritten":      len(edges), "hotnessAvailable": false, "parseDegraded": problems(degraded),
			"durationMs": ms(start),
		})
	})
	if err != nil {
		return Result{}, err
	}
	return Result{Status: status, FilesIndexed: indexed, FilesSkipped: skipped, Reason: "incremental"}, nil
}

// Resync moves the clone to the latest commit of the repository's default
// branch, then indexes what changed.
func (x *Indexer) Resync(ctx context.Context, repo uuid.UUID) (Result, error) {
	basics, err := x.q.IndexBasics(ctx, repo)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && basics.ClonePath == nil {
		return Result{Status: "degraded", Reason: "no_clone"}, nil
	}
	if err != nil {
		return Result{}, err
	}
	token, err := x.token()
	if err != nil {
		return Result{}, err
	}
	if err := git.Sync(ctx, *basics.ClonePath, basics.DefaultBranch, token); err != nil {
		return Result{Status: "degraded", Reason: "sync_failed:" + err.Error()}, nil
	}
	return x.Refresh(ctx, repo)
}

// writeGraph replaces the repository's import graph, resolves references
// through it, and replaces the ranks and the map.
func (x *Indexer) writeGraph(ctx context.Context, q *postgres.Queries, repo uuid.UUID, edges []Edge, ranks []FileRank, head string) error {
	if err := q.DeleteEdges(ctx, repo); err != nil {
		return err
	}
	edgeRows := make([]postgres.CopyEdgesParams, len(edges))
	for i, e := range edges {
		edgeRows[i] = postgres.CopyEdgesParams{RepoID: repo, FromFile: e.From, ToFile: e.To}
	}
	if _, err := q.CopyEdges(ctx, edgeRows); err != nil {
		return err
	}
	if err := q.ResolveDeclFiles(ctx, repo); err != nil {
		return err
	}
	if err := q.DeleteFileRanks(ctx, repo); err != nil {
		return err
	}
	rankRows := make([]postgres.CopyFileRanksParams, len(ranks))
	for i, r := range ranks {
		// rank is the PageRank alone: the clone is shallow, so there is no
		// history to measure how often a file changes (hotness).
		rankRows[i] = postgres.CopyFileRanksParams{RepoID: repo, FilePath: r.Path, Pagerank: r.PageRank, Rank: r.PageRank, Percentile: int16(r.Percentile)}
	}
	if _, err := q.CopyFileRanks(ctx, rankRows); err != nil {
		return err
	}
	rows, err := q.MapCandidates(ctx, repo)
	if err != nil {
		return err
	}
	candidates := make([]MapSymbol, len(rows))
	for i, r := range rows {
		candidates[i] = MapSymbol{Path: r.Path, Signature: *r.Signature}
	}
	text, tokens := RenderMap(candidates, MapTokenBudget)
	if err := q.DeleteRepoMaps(ctx, repo); err != nil {
		return err
	}
	if head == "" {
		return nil // no commit to cache it for
	}
	return q.PutRepoMap(ctx, postgres.PutRepoMapParams{RepoID: repo, CommitSha: head, TokenBudget: MapTokenBudget, MapText: text, TokenCount: int32(tokens)})
}

func (x *Indexer) saveState(ctx context.Context, q *postgres.Queries, repo uuid.UUID, sha, status string, indexed, skipped int, stats map[string]any) error {
	data, err := json.Marshal(stats)
	if err != nil {
		return err
	}
	return q.UpsertIndexState(ctx, postgres.UpsertIndexStateParams{
		RepoID: repo, LastIndexedSha: sha, IndexerVersion: indexerVersion, Status: status,
		FilesIndexed: int32(indexed), FilesSkipped: int32(skipped), Stats: data,
	})
}

// parsedFile is what the indexer read from one file.
type parsedFile struct {
	path      string
	hash      string
	symbols   []Symbol
	refs      []Reference
	endpoints []string
	crons     []string
	skipped   bool   // not read: past the time budget
	problem   string // why it couldn't be read
}

// parseFiles reads and parses files of the clone at dir, several at once.
// When start isn't zero, files not started within softBudget of it are
// skipped, and the second result is true.
func parseFiles(dir string, files []string, start time.Time) ([]parsedFile, bool) {
	out := make([]parsedFile, len(files))
	next := make(chan int)
	var wg sync.WaitGroup
	for range max(1, runtime.NumCPU()-1) {
		wg.Go(func() {
			for i := range next {
				out[i] = parseFile(dir, files[i])
			}
		})
	}
	over := false
	for i := range files {
		if !start.IsZero() && time.Since(start) > softBudget {
			over = true
			for j := i; j < len(files); j++ {
				out[j] = parsedFile{path: files[j], skipped: true}
			}
			break
		}
		next <- i
	}
	close(next)
	wg.Wait()
	return out, over
}

func parseFile(dir, file string) parsedFile {
	src, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(file)))
	if err != nil {
		// Deleted files too, as in TS: the index is then partial.
		return parsedFile{path: file, problem: err.Error()}
	}
	sum := sha1.Sum(src)
	return parsedFile{
		path: file, hash: hex.EncodeToString(sum[:]),
		symbols: ParseSymbols(file, src), refs: ParseReferences(file, src),
		endpoints: Endpoints(string(src)), crons: Crons(string(src)),
	}
}

func copyParsed(ctx context.Context, q *postgres.Queries, repo uuid.UUID, parsed []parsedFile) error {
	var syms []postgres.CopySymbolsParams
	var refs []postgres.CopyReferencesParams
	for _, p := range parsed {
		hash := p.hash
		for _, s := range p.symbols {
			line, end, sig := int32(s.Line), int32(s.EndLine), s.Signature
			syms = append(syms, postgres.CopySymbolsParams{
				RepoID: repo, Path: p.path, Name: clampName(s.Name), Kind: s.Kind, Line: &line, EndLine: &end,
				Exported: s.Exported, Signature: &sig, ContentHash: &hash,
			})
		}
		for _, r := range p.refs {
			refs = append(refs, postgres.CopyReferencesParams{RepoID: repo, FromPath: p.path, ToSymbol: clampName(r.Name), Line: int32(r.Line), ContentHash: &hash})
		}
	}
	if _, err := q.CopySymbols(ctx, syms); err != nil {
		return err
	}
	_, err := q.CopyReferences(ctx, refs)
	return err
}

func insertFacts(ctx context.Context, q *postgres.Queries, repo uuid.UUID, parsed []parsedFile) error {
	for _, p := range parsed {
		if len(p.endpoints) == 0 && len(p.crons) == 0 {
			continue
		}
		endpoints, _ := json.Marshal(orEmpty(p.endpoints))
		crons, _ := json.Marshal(orEmpty(p.crons))
		if err := q.InsertFileFacts(ctx, postgres.InsertFileFactsParams{RepoID: repo, FilePath: p.path, Endpoints: endpoints, Crons: crons}); err != nil {
			return err
		}
	}
	return nil
}

// clampName cuts a name to maxNameLen UTF-16 code units.
func clampName(s string) string {
	units := utf16.Encode([]rune(s))
	if len(units) <= maxNameLen {
		return s
	}
	return string(utf16.Decode(units[:maxNameLen]))
}

type parseProblem struct {
	File   string `json:"file"`
	Reason string `json:"reason"`
}

// maxProblems is how many parse problems the stats keep.
const maxProblems = 50

func appendProblem(list []parseProblem, p parsedFile) []parseProblem {
	if len(list) >= maxProblems || p.problem == "" {
		return list
	}
	return append(list, parseProblem{File: p.path, Reason: p.problem})
}

func problems(list []parseProblem) []parseProblem {
	if list == nil {
		return []parseProblem{}
	}
	return list
}

func walkStats(w WalkStats) map[string]any {
	return map[string]any{"totalCandidates": w.TotalCandidates, "skippedTooLarge": w.SkippedTooLarge, "bounded": w.Bounded}
}

func count(parsed []parsedFile, f func(parsedFile) int) int {
	n := 0
	for _, p := range parsed {
		n += f(p)
	}
	return n
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func ms(start time.Time) int64 { return time.Since(start).Milliseconds() }
