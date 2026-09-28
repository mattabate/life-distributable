package threads

import "testing"

// `answered` = the ball is with the agent, for one turn. A message to the
// session answers what blocks it, never the owner's own dated steps; and a
// turn that ends without closing an answered card hands it back — open, on
// the owner's board.
func TestAnsweredLastsOneTurn(t *testing.T) {
	m, _, _ := setup(t)
	th, err := m.Create("", "life", "", "Tonight's to-dos.", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	complete(t, m, th.ID, "NEEDS YOU: order the new router")
	step, err := m.AddAskAs(th.ID, "", "Buy new sneakers", "", "physical", "", "", ClassStep)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Send(th.ID, "did you read the card's terms?"); err != nil {
		t.Fatal(err)
	}
	state := func() (order, sneakers string) {
		asks, _ := m.ListAsks("active", th.ID, 10)
		for _, a := range asks {
			if a.ID == step.ID {
				sneakers = a.State
			} else if a.Kind != "read" {
				order = a.State
			}
		}
		return
	}
	if o, s := state(); o != "answered" || s != "open" {
		t.Fatalf("after the owner's message: order=%s step=%s", o, s)
	}
	// A reply THROUGH the step's card still answers it.
	m.markAnswered(th.ID, "ask:"+step.ID)
	if _, s := state(); s != "answered" {
		t.Fatalf("targeted reply to a step: %s", s)
	}
	// The turn answers the question and closes neither card: both are the owner's again.
	complete(t, m, th.ID, "The terms check out.")
	if o, s := state(); o != "open" || s != "open" {
		t.Fatalf("after the turn: order=%s step=%s", o, s)
	}
	if got, _ := m.Get(th.ID); got.Status != "needs_you" {
		t.Fatalf("status %s", got.Status)
	}
}
