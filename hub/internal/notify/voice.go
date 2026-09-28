package notify

import (
	"errors"
	"log"
	"strings"
	"time"
)

// A push is heard before it is read (Announce Notifications on the phone,
// speakOnMac on the laptop), and "To read — <card title>" is a label, not a
// sentence, and it should sound conversational, not robotic. So a card can
// carry the line to SAY, written by the session that raised it (`lifectl ask
// add … --say`) — it knows what was asked for and what it did, and it costs
// no extra call. (Having a model reword every card inside the hub was slow,
// cost money per push, and spoke about "your last message", which means
// nothing over headphones.) The message IS the reply — the card leads with
// it — so it may say the whole thing; the steps the owner carries out stay
// on the card, unspoken.
//
// voiceMax is a SANITY BOUND, not the target: sessions are told ≤700
// (threads.go, the heard reminder — about a minute of speech), and a line
// past the bound is truncated rune-safe (apns.push), never swapped for the
// label form: a refused line used to go out as "To read. <cut title>.
// <session name>", which is not the message.
const voiceMax = 1500 // characters; about two minutes of speech

// cleanVoice makes a spoken line safe to say: one paragraph, no quotes or
// markdown marks around it; only an empty line is refused — length is
// apns.push's business (truncate, never label).
func cleanVoice(out string) (string, error) {
	s := strings.Join(strings.Fields(out), " ")
	s = strings.Trim(s, "\"“”'`*_ ")
	if s == "" {
		return "", errors.New("empty line")
	}
	return s, nil
}

// PushCard is Push for a card: with a spoken line, that sentence is the whole
// alert — no "To read" label for Siri to read first. Without one (or with one
// that cannot be said) the label form goes out as before. `thread` is the
// session the card belongs to: while the line is heard, the Sessions list
// marks that session "speaking" (Voice), so the voice just heard has a row
// to click into. `card` is
// the card's id (an ask or a proposal): its line is kept in voice_queue until
// it is heard, and spoken again by a hub that restarted over it (queue.go).
func (a *APNs) PushCard(kind, line, say, thread, card string) error {
	label := "Needs you"
	if kind == "read" {
		label = "To read"
	} else {
		kind = "needs_you"
	}
	if say != "" {
		said, err := cleanVoice(say)
		if err == nil {
			return a.push(kind, "", said, thread, card)
		}
		log.Printf("voice: %v; sending the label form", err)
	}
	return a.push(kind, label, line, thread, card)
}

// Voice is told which session is being heard and until when, and which has a
// line queued for its turn (threads.Voice); nil marks nothing.
type Voice interface {
	Mark(thread string, until time.Time)
	Wait(thread, card string) (release func())
}
