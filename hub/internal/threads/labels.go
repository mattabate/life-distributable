package threads

import (
	"strconv"
	"strings"
)

// The words a session card prints, decided once here so the console and the
// phone print the same ones: same numbers and words, different rendering.
// When each client spelled them itself, an idle session read "idle" on the
// laptop and "message" on the phone.

// Pill is one capsule on a session card: its word and its tone. Tone is one of
// needs (red), read (blue), install (teal), running (blue, pulsing), done
// (grey) or idle (plain) — the console's pill class and the phone's tint.
type Pill struct {
	Word string `json:"word"`
	Tone string `json:"tone"`
}

// StatusPill: the pill a session wears when no card of the owner's is open on it.
func StatusPill(status string) Pill {
	switch status {
	case "needs_you":
		return Pill{"your turn", "needs"}
	case "running":
		return Pill{"running", "running"}
	case "done", "archived":
		return Pill{"done", "done"}
	case "":
		return Pill{"idle", "idle"}
	}
	return Pill{strings.ReplaceAll(status, "_", " "), "idle"}
}

// CardsPills: the capsules a session's open cards earn it, ONE PER CLASS in
// the order the owner works them — red "N for you" for what blocks the
// session (decisions, access, steps, approvals), blue "N to read" for
// answers, teal "N to install" for builds. One blended pill would hide what
// the session actually waits on (a read beside an install reading "2 for
// you"), and that makes it harder to allocate time. Zero cards → no pills.
func CardsPills(n, reads, installs int) []Pill {
	var out []Pill
	if acts := n - reads - installs; acts > 0 {
		out = append(out, Pill{strconv.Itoa(acts) + " for you", "needs"})
	}
	if reads > 0 {
		out = append(out, Pill{strconv.Itoa(reads) + " to read", "read"})
	}
	if installs > 0 {
		out = append(out, Pill{strconv.Itoa(installs) + " to install", "install"})
	}
	return out
}

// ModelLabel: "claude-fable-5-1[1m]" → "fable 5.1", "claude-opus-4-5-20251101"
// → "opus 4.5". "" stays "".
func ModelLabel(id string) string {
	if id == "" || id == "owner" || id == "unknown" {
		return id
	}
	m := strings.TrimSuffix(strings.TrimPrefix(id, "claude-"), "[1m]")
	parts := strings.Split(m, "-")
	if last := parts[len(parts)-1]; len(parts) > 1 && len(last) == 8 && strings.Trim(last, "0123456789") == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return parts[0] + " " + strings.Join(parts[1:], ".")
}

// ScheduleLabel: a cadence in words, "weekly@Sun 17:30" → "weekly Sun 17:30".
func ScheduleLabel(s string) string { return strings.Replace(s, "@", " ", 1) }
