package repointel

import (
	"errors"
	"math"
	"slices"
)

// FileRank is how depended-on a file is: its PageRank over the import
// graph, and the percentile of that among the repository's files.
type FileRank struct {
	Path       string
	PageRank   float64
	Percentile int // 0 to 100; files with the same rank share one
}

// PageRank settings, graphology's defaults, which the TS indexer used.
const (
	damping       = 0.85
	maxIterations = 100
	tolerance     = 1e-6
)

var errNoConvergence = errors.New("pagerank: failed to converge")

// RankFiles ranks files by PageRank over the import edges, so files that
// much of the repository depends on rank high. Edges between files not in
// files, and self-imports, are left out. It computes exactly what
// graphology's pagerank computes, in the same order, so the scores are the
// TS server's to the last bit; when that doesn't converge, every file gets
// the same score.
func RankFiles(files []string, edges []Edge) []FileRank {
	if len(files) == 0 {
		return nil
	}
	index := make(map[string]int, len(files))
	for i, f := range files {
		index[f] = i
	}
	out := make([][]int, len(files)) // each file's imports, by index, once
	seen := map[[2]int]bool{}
	for _, e := range edges {
		from, ok1 := index[e.From]
		to, ok2 := index[e.To]
		if !ok1 || !ok2 || from == to || seen[[2]int{from, to}] {
			continue
		}
		seen[[2]int{from, to}] = true
		out[from] = append(out[from], to)
	}

	scores, err := pagerank(out)
	if err != nil {
		scores = make([]float64, len(files))
		for i := range scores {
			scores[i] = 1 / float64(len(files))
		}
	}
	ranks := make([]FileRank, len(files))
	for i, f := range files {
		ranks[i] = FileRank{Path: f, PageRank: scores[i]}
	}

	// A file's percentile is the share of files ranked at most as high, so
	// the top file gets 100 and ties share the value of the last among them.
	order := make([]int, len(files))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		switch {
		case scores[a] < scores[b]:
			return -1
		case scores[a] > scores[b]:
			return 1
		}
		return 0
	})
	n := len(order)
	for i := 0; i < n; {
		j := i
		for j+1 < n && scores[order[j+1]] == scores[order[i]] {
			j++
		}
		pct := int(math.Round(float64(100*(j+1)) / float64(n)))
		for k := i; k <= j; k++ {
			ranks[order[k]].Percentile = pct
		}
		i = j + 1
	}
	return ranks
}

// pagerank is graphology-metrics' pagerank on a graph whose node i links to
// out[i], every edge weighing 1. The products are converted to float64
// before they are added, which keeps Go from fusing a multiply and an add
// into one instruction and rounding differently from JavaScript.
func pagerank(out [][]int) ([]float64, error) {
	// A variable, not the constant: Go computes 1-damping exactly at compile
	// time (0.15), JavaScript in float64 at run time (0.15000000000000002).
	alpha := float64(damping)
	teleport := 1 - alpha
	n := len(out)
	p := 1 / float64(n)
	x := make([]float64, n)
	var dangling []int
	for i := range out {
		x[i] = p
		if len(out[i]) == 0 {
			dangling = append(dangling, i)
		}
	}
	for range maxIterations {
		last := x
		x = make([]float64, n)
		dangleSum := 0.0
		for _, i := range dangling {
			dangleSum += last[i]
		}
		dangleSum *= alpha
		for i := range out {
			w := 1 / float64(len(out[i]))
			for _, to := range out[i] {
				x[to] += float64(float64(alpha*last[i]) * w)
			}
			x[i] += float64(dangleSum*p) + float64(teleport*p)
		}
		sum := 0.0
		for i := range x {
			sum += math.Abs(x[i] - last[i])
		}
		if sum < float64(n)*tolerance {
			return x, nil
		}
	}
	return nil, errNoConvergence
}
