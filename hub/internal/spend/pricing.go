package spend

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

// Price per million tokens, USD. Source: Anthropic first-party API rates.
// Consumer Max/Pro plans are flat-rate, so these are "API-equivalent" costs —
// useful as a relative measure of usage, not a bill.
type Price struct {
	Input, Output float64
	// CacheRead $/MTok when it differs from the default Input*cacheRead;
	// 0 = use the default.
	CacheRead float64
}

// prices.json is the ONE table: the hub embeds it and ops/hublib.py reads it,
// so an audit script can no longer drift from the dashboard (review
// 2026-09-26: spend-audit priced opus-4 at 5/25, context-cost had no
// opus-5-5). Update prices there.
//
//go:embed prices.json
var pricesJSON []byte

type priceRow struct {
	Prefix    string  `json:"prefix"`
	Input     float64 `json:"input"`
	Output    float64 `json:"output"`
	CacheRead float64 `json:"cache_read"`
}

var (
	// Longest prefix first where one id contains another: "claude-fable-5"
	// would otherwise swallow "claude-fable-5-1".
	prices       []priceRow
	unknownPrice Price
	cacheRead    float64
	cacheWrite5m float64
	cacheWrite1h float64
)

func init() {
	var t struct {
		Cache struct {
			Read    float64 `json:"read"`
			Write5m float64 `json:"write_5m"`
			Write1h float64 `json:"write_1h"`
		} `json:"cache"`
		Unknown priceRow   `json:"unknown"`
		Models  []priceRow `json:"models"`
	}
	if err := json.Unmarshal(pricesJSON, &t); err != nil || len(t.Models) == 0 {
		panic(fmt.Sprintf("spend: bad prices.json (%d models): %v", len(t.Models), err))
	}
	prices = t.Models
	unknownPrice = Price{t.Unknown.Input, t.Unknown.Output, t.Unknown.CacheRead}
	cacheRead, cacheWrite5m, cacheWrite1h = t.Cache.Read, t.Cache.Write5m, t.Cache.Write1h
}

func priceFor(model string) (Price, bool) {
	for _, e := range prices {
		if strings.HasPrefix(model, e.Prefix) {
			return Price{e.Input, e.Output, e.CacheRead}, true
		}
	}
	return unknownPrice, false // unknown: assume Opus tier, flag it
}

// Cost in USD for one usage record.
func (u Usage) Cost() (usd float64, known bool) {
	p, known := priceFor(u.Model)
	in := p.Input / 1e6
	cr := in * cacheRead
	if p.CacheRead > 0 {
		cr = p.CacheRead / 1e6
	}
	usd = float64(u.Input)*in +
		float64(u.CacheRead)*cr +
		float64(u.CacheWrite5m)*in*cacheWrite5m +
		float64(u.CacheWrite1h)*in*cacheWrite1h +
		float64(u.Output)*p.Output/1e6
	return usd, known
}
