package notify

import "strings"

// The one push funnel: an ask, a proposal and a calendar nag each used to
// reach the owner through their own copy of "card lane if the notifier has
// one, else the label push" and their own spoken sentence. Card is that
// choice, written once; Spoken is the sentence.
//
// The notifier is whatever the package was handed (the scheduler in the live
// hub, a stub in tests): only NeedsYou is required, ToRead and Card are lanes
// it may also have.

// Needer is the least a notifier has: the NEEDS YOU push.
type Needer interface {
	NeedsYou(string) error
}

// Carder is the card lane (sched.Scheduler → APNs.PushCard): the push speaks
// `say`, marks `thread` speaking, and keeps `card` queued until heard.
type Carder interface {
	Card(kind, line, say, thread, card string) error
}

// Reader is the quieter "To read" push.
type Reader interface {
	ToRead(string) error
}

// Card sends one card to the owner through the best lane n has. `line` is the
// label form (what a notifier without the card lane shows); `say` the
// sentence the card lane speaks ("" = the label form there too). said is the
// sentence the card lane was handed — "" when no card lane took it, so a
// caller that keeps what was heard on the row stores nothing for a push that
// spoke nothing. A read card without the card lane goes out as "To read" or
// not at all: a note never buzzes as NEEDS YOU.
func Card(n Needer, kind, line, say, thread, card string) (said string, err error) {
	if n == nil {
		return "", nil
	}
	if c, ok := n.(Carder); ok {
		return say, c.Card(kind, line, say, thread, card)
	}
	if kind == "read" {
		if r, ok := n.(Reader); ok {
			return "", r.ToRead(line)
		}
		return "", nil
	}
	return "", n.NeedsYou(line)
}

// Spoken is what a card with no `--say` speaks: one conversational sentence,
// made from its title and its session's name, both already plain text (the
// caller strips its own markup) — never a label form read in parts like
// "To read. <title>. <session>". The owner's own dated work (a step, a
// practice) is a reminder, not a session talking; a proposal asks for
// approval, with no id in it.
func Spoken(kind, class, title, thread string) string {
	title = sentence(title)
	if kind == "approval" {
		if title == "" {
			return "Hey, something is waiting for your approval."
		}
		return "Hey, I need your approval. " + title
	}
	if r := []rune(thread); len(r) > 80 {
		thread = strings.TrimSpace(string(r[:80]))
	}
	switch {
	case class == "step" || class == "practice":
		return "Hey, a reminder. " + title
	case thread == "":
		return "Hey, " + title
	case kind == "read":
		return "Hey, about " + thread + ". " + title
	case kind == "error":
		return "Hey, " + thread + " stopped on an error. " + title
	}
	return "Hey, I need you on " + thread + ". " + title
}

// sentence ends s with a stop unless it already has one.
func sentence(s string) string {
	if s != "" && !strings.ContainsAny(s[len(s)-1:], ".!?") && !strings.HasSuffix(s, "…") {
		s += "."
	}
	return s
}
