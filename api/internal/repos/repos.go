// Package repos adds, refreshes and removes the GitHub repositories a
// workspace reviews, and keeps a clone of each.
package repos

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DimaMaimesko/dev-digest/api/internal/git"
	"github.com/DimaMaimesko/dev-digest/api/internal/jobs"
	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
)

// Ref names a GitHub repository.
type Ref struct {
	Owner, Name string
}

// FullName is "owner/name".
func (r Ref) FullName() string { return r.Owner + "/" + r.Name }

// GitHub's rules for names: an account is letters, digits and hyphens, not
// starting with one; a repository is letters, digits, ".", "-" and "_".
var (
	ownerName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)
	repoName  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
)

// ErrBadURL is an URL that doesn't name a GitHub repository.
var ErrBadURL = errors.New("not a GitHub repository URL")

// ParseURL reads the repository a GitHub URL names, such as
// https://github.com/vercel/next.js or https://github.com/o/r.git.
func ParseURL(raw string) (Ref, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		(u.Hostname() != "github.com" && u.Hostname() != "www.github.com") {
		return Ref{}, ErrBadURL
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 {
		return Ref{}, ErrBadURL
	}
	r := Ref{Owner: parts[0], Name: strings.TrimSuffix(parts[1], ".git")}
	if !ownerName.MatchString(r.Owner) || !repoName.MatchString(r.Name) || r.Name == "." || r.Name == ".." {
		return Ref{}, ErrBadURL
	}
	return r, nil
}

// cloneDepth is how many commits a clone has: the last one.
const cloneDepth = 1

// Config is what a Store needs.
type Config struct {
	DB       *pgxpool.Pool
	Jobs     *jobs.Runner
	CloneDir string                 // clones go in CloneDir/owner/name
	Token    func() (string, error) // the GitHub token, read when a clone starts; "" for none
	// Remote is where repositories are cloned from, owner/name.git being
	// added to it; "" means "https://github.com/".
	Remote string
}

// Store changes a workspace's repositories.
type Store struct {
	cfg Config
	q   *postgres.Queries
}

// NewStore returns a Store.
func NewStore(cfg Config) *Store {
	if cfg.Remote == "" {
		cfg.Remote = "https://github.com/"
	}
	return &Store{cfg: cfg, q: postgres.New(cfg.DB)}
}

// Add adds a repository to the workspace and clones it in the background.
// created is false when the workspace had it already; then nothing is
// cloned.
func (s *Store) Add(ctx context.Context, workspace, user uuid.UUID, ref Ref) (repo postgres.Repo, created bool, err error) {
	repo, err = s.q.InsertRepo(ctx, postgres.InsertRepoParams{
		WorkspaceID: workspace, Owner: ref.Owner, Name: ref.Name, FullName: ref.FullName(), CreatedBy: &user,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		repo, err = s.q.RepoByFullName(ctx, postgres.RepoByFullNameParams{WorkspaceID: workspace, FullName: ref.FullName()})
		return repo, false, err
	}
	if err != nil {
		return repo, false, err
	}
	return repo, true, s.clone(ctx, repo)
}

// ErrNotFound means the workspace has no such repository.
var ErrNotFound = errors.New("repository not found")

// Refresh fetches the latest commits into a repository's clone, or clones
// it again when it has none, in the background.
func (s *Store) Refresh(ctx context.Context, workspace, id uuid.UUID) error {
	repo, err := s.q.GetRepo(ctx, postgres.GetRepoParams{WorkspaceID: workspace, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return s.clone(ctx, repo)
}

// Remove removes a repository from the workspace, with its pull requests
// and reviews. Its clone stays on disk, as in TS: adding it again fetches
// into it.
func (s *Store) Remove(ctx context.Context, workspace, id uuid.UUID) error {
	n, err := s.q.DeleteRepo(ctx, postgres.DeleteRepoParams{WorkspaceID: workspace, ID: id})
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

// Dir is where a repository's clone is.
func (s *Store) Dir(ref Ref) string { return filepath.Join(s.cfg.CloneDir, ref.Owner, ref.Name) }

// clone enqueues the job that clones or fetches repo.
func (s *Store) clone(ctx context.Context, repo postgres.Repo) error {
	ref := Ref{Owner: repo.Owner, Name: repo.Name}
	remote := s.cfg.Remote + ref.FullName() + ".git"
	payload := map[string]string{"repoId": repo.ID.String(), "owner": ref.Owner, "name": ref.Name, "url": remote}
	_, err := s.cfg.Jobs.Enqueue(ctx, repo.WorkspaceID, "clone", payload, func(ctx context.Context) error {
		token, err := s.cfg.Token()
		if err != nil {
			return err
		}
		dir := s.Dir(ref)
		if err := git.Clone(ctx, dir, remote, token, cloneDepth); err != nil {
			return fmt.Errorf("clone %s: %w", ref.FullName(), err)
		}
		return s.q.SetClonePath(context.WithoutCancel(ctx), postgres.SetClonePathParams{ID: repo.ID, ClonePath: &dir})
	})
	return err
}
