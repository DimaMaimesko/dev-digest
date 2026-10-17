package tour

import (
	"context"
	"sync"

	"github.com/DimaMaimesko/dev-digest/api/internal/review"
)

// fakeLLM is a hand-written review.LLM. It scripts an answer (or a sequence
// of answers) per SchemaName, records every request it was sent, and can
// block until its context is cancelled, to exercise a timeout or a
// shutdown.
type fakeLLM struct {
	mu sync.Mutex

	// answers, keyed by SchemaName, is the queue of texts to answer with,
	// oldest first. Each call to CompleteJSON for that schema pops one;
	// the last one repeats once the queue is empty.
	answers map[string][]string
	// errs, keyed by SchemaName, behaves like answers but makes
	// CompleteJSON return an error instead.
	errs map[string][]error
	// block, keyed by SchemaName, makes CompleteJSON wait for ctx to be
	// done (a timeout or a cancel) before returning ctx.Err().
	block map[string]bool
	// cost is attached to every successful answer's usage, when set.
	cost *float64

	requests []review.JSONRequest
}

func newFakeLLM() *fakeLLM {
	return &fakeLLM{
		answers: map[string][]string{},
		errs:    map[string][]error{},
		block:   map[string]bool{},
	}
}

func (f *fakeLLM) script(schema string, texts ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers[schema] = append(f.answers[schema], texts...)
}

func (f *fakeLLM) scriptErr(schema string, errs ...error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errs[schema] = append(f.errs[schema], errs...)
}

func (f *fakeLLM) blockOn(schema string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.block[schema] = true
}

func (f *fakeLLM) CompleteJSON(ctx context.Context, req review.JSONRequest) (review.JSONResponse, error) {
	f.mu.Lock()
	f.requests = append(f.requests, req)
	blocking := f.block[req.SchemaName]
	f.mu.Unlock()

	if blocking {
		<-ctx.Done()
		return review.JSONResponse{}, ctx.Err()
	}

	f.mu.Lock()
	if errs := f.errs[req.SchemaName]; len(errs) > 0 {
		next := errs[0]
		if len(errs) > 1 {
			f.errs[req.SchemaName] = errs[1:]
		}
		f.mu.Unlock()
		return review.JSONResponse{TokensIn: 10}, next
	}
	texts := f.answers[req.SchemaName]
	f.mu.Unlock()

	if len(texts) == 0 {
		return review.JSONResponse{Text: "{}", TokensIn: 10, TokensOut: 10, CostUSD: f.cost}, nil
	}
	text := texts[0]
	if len(texts) > 1 {
		f.mu.Lock()
		f.answers[req.SchemaName] = texts[1:]
		f.mu.Unlock()
	}
	return review.JSONResponse{Text: text, TokensIn: 10, TokensOut: 10, CostUSD: f.cost}, nil
}

func (f *fakeLLM) requestsFor(schema string) []review.JSONRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []review.JSONRequest
	for _, r := range f.requests {
		if r.SchemaName == schema {
			out = append(out, r)
		}
	}
	return out
}
