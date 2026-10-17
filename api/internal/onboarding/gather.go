package onboarding

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/DimaMaimesko/dev-digest/api/internal/git"
	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
	"github.com/DimaMaimesko/dev-digest/api/internal/tour"
)

// Caps on what gather reads, beyond tour's own per-call budget: how much of
// a secret-free file internal/onboarding reads, how many such files, how
// many TODO/FIXME matches it scans, and how many ranked files it fetches
// from the index (Approach -> Domain, Section inputs; Clarifications Q8/Q9).
const (
	maxBlobBytes   = 64 * 1024
	maxDocsFiles   = 20
	maxTodoScan    = 200
	maxRankedFiles = 40
)

// gather collects every input tour.Generate needs for repoID at commit, in
// the clone at dir: the tree, the allowlisted docs and the root README, the
// TODO/FIXME scan, the repository map, the top-ranked files and the
// routes/crons facts. It reads nothing tour.Excluded denies, and only what
// tour.Readable allows (Q8).
func (s *Store) gather(ctx context.Context, dir, commit string, repoID uuid.UUID) (tour.Inputs, error) {
	entries, err := git.ListTree(ctx, dir, commit)
	if err != nil {
		return tour.Inputs{}, fmt.Errorf("list tree: %w", err)
	}

	docs, readme := s.readDocs(ctx, dir, commit, entries)

	// A failed TODO/FIXME scan doesn't fail the whole tour: first_tasks
	// loses one of its inputs, but the other four sections, and
	// first_tasks's remaining inputs (ranked files, tree), are unaffected.
	todos, err := git.GrepTodos(ctx, dir, commit, maxTodoScan)
	if err != nil {
		s.log.Warn("onboarding todo scan failed, continuing without todos", "err", err)
		todos = nil
	}

	m, err := s.index.Map(ctx, repoID)
	if err != nil {
		return tour.Inputs{}, fmt.Errorf("repo map: %w", err)
	}

	ranked, err := s.q.TopRankedFiles(ctx, postgres.TopRankedFilesParams{RepoID: repoID, Limit: maxRankedFiles})
	if err != nil {
		return tour.Inputs{}, fmt.Errorf("top ranked files: %w", err)
	}
	rankedPaths := make([]string, len(ranked))
	for i, row := range ranked {
		rankedPaths[i] = row.FilePath
	}

	routes, err := s.q.FileFactsWithRoutes(ctx, postgres.FileFactsWithRoutesParams{RepoID: repoID, Limit: maxRankedFiles})
	if err != nil {
		return tour.Inputs{}, fmt.Errorf("file facts: %w", err)
	}

	return tour.Inputs{
		Tree: toEntries(entries), RepoMap: m.Text, README: readme, Docs: docs,
		RankedFiles: rankedPaths, RoutesCrons: renderRoutesCrons(routes), Todos: filterTodos(todos),
	}, nil
}

// toEntries converts git.ListTree's shape to tour.Entry, the shape the
// domain package takes so it never imports internal/git.
func toEntries(list []git.TreeEntry) []tour.Entry {
	out := make([]tour.Entry, len(list))
	for i, e := range list {
		out[i] = tour.Entry{Path: e.Path, Dir: e.Dir}
	}
	return out
}

// filterTodos drops any TODO/FIXME match in a file tour.Excluded denies
// (AC-40), and converts the rest to tour.TodoRef.
func filterTodos(todos []git.Todo) []tour.TodoRef {
	out := make([]tour.TodoRef, 0, len(todos))
	for _, t := range todos {
		if tour.Excluded(t.Path) {
			continue
		}
		out = append(out, tour.TodoRef{Path: t.Path, Line: t.Line, Text: t.Text})
	}
	return out
}

// readDocs reads at most maxDocsFiles allowlisted files (Readable &&
// !Excluded) at commit, and returns the root README's text separately
// (""  when there isn't one). Candidates are ordered root README first,
// then by path depth (shallower first), then by path: a repository with
// many deep, allowlisted docs never pushes the root's own manifests
// (go.mod, package.json, Makefile, ...) out of maxDocsFiles.
func (s *Store) readDocs(ctx context.Context, dir, commit string, entries []git.TreeEntry) (docs []tour.Doc, readme string) {
	var candidates []string
	for _, e := range entries {
		if e.Dir {
			continue
		}
		if tour.Excluded(e.Path) || !tour.Readable(e.Path) {
			continue
		}
		candidates = append(candidates, e.Path)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		iReadme, jReadme := isRootReadme(candidates[i]), isRootReadme(candidates[j])
		if iReadme != jReadme {
			return iReadme
		}
		di, dj := pathDepth(candidates[i]), pathDepth(candidates[j])
		if di != dj {
			return di < dj
		}
		return candidates[i] < candidates[j]
	})
	if len(candidates) > maxDocsFiles {
		candidates = candidates[:maxDocsFiles]
	}

	for _, p := range candidates {
		data, _, err := git.ReadBlob(ctx, dir, commit, p, maxBlobBytes)
		if err != nil {
			s.log.Warn("onboarding doc not read", "path", p, "err", err)
			continue
		}
		text := string(data)
		docs = append(docs, tour.Doc{Path: p, Text: text})
		if readme == "" && isRootReadme(p) {
			readme = text
		}
	}
	return docs, readme
}

// isRootReadme reports whether p is a README at the repository's root.
func isRootReadme(p string) bool {
	return pathDepth(p) == 0 && strings.HasPrefix(path.Base(p), "README")
}

// pathDepth is how many directory levels p is below the repository root.
func pathDepth(p string) int {
	p = strings.Trim(p, "/")
	if p == "" {
		return 0
	}
	return strings.Count(p, "/")
}

// renderRoutesCrons renders file_facts rows with routes or crons as a short
// line per file, for the architecture and critical_paths inputs.
func renderRoutesCrons(rows []postgres.FileFactsWithRoutesRow) string {
	var lines []string
	for _, r := range rows {
		var endpoints, crons []string
		_ = json.Unmarshal(r.Endpoints, &endpoints)
		_ = json.Unmarshal(r.Crons, &crons)

		var parts []string
		if len(endpoints) > 0 {
			parts = append(parts, "routes: "+strings.Join(endpoints, ", "))
		}
		if len(crons) > 0 {
			parts = append(parts, "crons: "+strings.Join(crons, ", "))
		}
		if len(parts) == 0 {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s — %s", r.FilePath, strings.Join(parts, "; ")))
	}
	return strings.Join(lines, "\n")
}
