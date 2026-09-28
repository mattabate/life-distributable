package store

// Outcome is one button on a card, worded by the hub so both surfaces draw
// the same row (a read card says "Read it", a decision "Decided"). Value is
// the prompts `outcome` the button posts ("" = words only, the card stays
// open); Label is what the button says. Asks, proposals and recs all carry
// `outcomes` in this shape, decisive ones first and "Reply" ("") last —
// the card's row and the composer's chips draw exactly this order — so neither client keeps a vocabulary or an order of its own. The
// silent close (Dismiss, or Read on a read card) is not an outcome: it says
// nothing to anyone.
type Outcome struct {
	Value string `json:"value"`
	Label string `json:"label"`
	// Hint: what pressing it does, in one line (the console's tooltip). Only
	// the rec and tick sets carry one (store/close.go).
	Hint string `json:"hint,omitempty"`
}
