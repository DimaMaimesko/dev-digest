package repointel

import (
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
)

// Limits of an index.
const (
	maxIndexedFiles = 5000
	maxIndexedSize  = 400 * 1024 // bytes; bigger files are skipped
)

// indexedExt are the extensions the indexer reads, in any case.
var indexedExt = map[string]bool{".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".cjs": true}

// excludedDirs are directories the indexer doesn't enter, at any depth.
var excludedDirs = map[string]bool{"node_modules": true, "dist": true, "build": true, "coverage": true,
	".next": true, "out": true, "vendor": true, ".git": true}

// WalkStats counts what a walk left out.
type WalkStats struct {
	TotalCandidates int `json:"totalCandidates"` // files with an indexed extension
	SkippedTooLarge int `json:"skippedTooLarge"`
	Bounded         int `json:"bounded"` // left out past maxIndexedFiles
}

// Walk lists the files of the clone at dir the indexer reads, relative to
// it and sorted: TypeScript and JavaScript files, outside excludedDirs, up
// to 400 KB, the first 5000. Symbolic links aren't followed.
func Walk(dir string) ([]string, WalkStats) {
	var files []string
	var stats WalkStats
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return nil // unreadable: skipped
		case d.IsDir():
			if p != dir && excludedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		case !d.Type().IsRegular() || !indexedExt[strings.ToLower(filepath.Ext(p))]:
			return nil
		}
		stats.TotalCandidates++
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.Size() > maxIndexedSize {
			stats.SkippedTooLarge++
			return nil
		}
		if rel, err := filepath.Rel(dir, p); err == nil {
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	slices.Sort(files)
	if len(files) > maxIndexedFiles {
		stats.Bounded = len(files) - maxIndexedFiles
		files = files[:maxIndexedFiles]
	}
	return files, stats
}
