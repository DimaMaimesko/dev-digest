package review

// Usage is what model calls took: tokens, and cost when the provider reports
// it.
type Usage struct {
	TokensIn  int
	TokensOut int
	// CostUSD is the sum of the costs the calls reported, in US dollars, or
	// nil when none reported one.
	CostUSD *float64
}

// Add counts one call's answer.
func (u *Usage) Add(res JSONResponse) {
	u.TokensIn += res.TokensIn
	u.TokensOut += res.TokensOut
	u.addCost(res.CostUSD)
}

// Plus returns u and v together.
func (u Usage) Plus(v Usage) Usage {
	u.TokensIn += v.TokensIn
	u.TokensOut += v.TokensOut
	u.addCost(v.CostUSD)
	return u
}

func (u *Usage) addCost(cost *float64) {
	if cost == nil {
		return
	}
	sum := *cost
	if u.CostUSD != nil {
		sum += *u.CostUSD
	}
	u.CostUSD = &sum
}
