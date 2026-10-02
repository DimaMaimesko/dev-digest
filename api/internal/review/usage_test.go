package review

import "testing"

func TestUsage(t *testing.T) {
	cost := func(f float64) *float64 { return &f }

	var u Usage
	u.Add(JSONResponse{TokensIn: 100, TokensOut: 10})
	if u.CostUSD != nil {
		t.Fatalf("CostUSD = %v after a call with no cost, want nil", *u.CostUSD)
	}
	u.Add(JSONResponse{TokensIn: 100, TokensOut: 10, CostUSD: cost(0.25)})
	u = u.Plus(Usage{TokensIn: 1, TokensOut: 2, CostUSD: cost(0.5)})
	u = u.Plus(Usage{TokensIn: 1})
	if u.TokensIn != 202 || u.TokensOut != 22 || u.CostUSD == nil || *u.CostUSD != 0.75 {
		t.Errorf("got %d/%d tokens, cost %v; want 202/22 and 0.75", u.TokensIn, u.TokensOut, u.CostUSD)
	}

	free := Usage{}.Plus(Usage{CostUSD: cost(0)})
	if free.CostUSD == nil || *free.CostUSD != 0 {
		t.Errorf("a free call's cost = %v, want 0, not unknown", free.CostUSD)
	}
}
