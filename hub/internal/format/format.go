// Package format is the one money and count wording the hub, the console
// (ui.js) and the phone (Format.swift) share. The rules are pinned by
// shared/format-cases.json, which all three test suites read, so a label
// the hub words and a number a client words cannot disagree (2026-09-14:
// the console printed "$39,680.12" and "-$3.00" where the phone printed
// "$39,680" and "−$3").
package format

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

const minus = "−" // U+2212, never an ASCII hyphen before a dollar sign

// USD is a balance or a price: whole dollars from $100 up, cents below —
// and no ".00" on a whole amount, so a $15 subscription is "$15", not "$15.00".
func USD(v float64) string {
	a := math.Abs(v)
	var s string
	if cents := math.Round(a * 100); cents >= 10000 || math.Mod(cents, 100) == 0 {
		s = Commas(int64(math.Round(a)))
	} else {
		s = strconv.FormatFloat(cents/100, 'f', 2, 64)
	}
	return sign(v, s)
}

// USDWhole is always whole dollars: an estimate ("≈$63 left") or a tally.
func USDWhole(v float64) string { return sign(v, Commas(int64(math.Round(math.Abs(v))))) }

// USDSigned carries an explicit sign, for a gain or a flow: "+$1,234", "−$12.50".
func USDSigned(v float64) string {
	s := USD(v)
	if strings.HasPrefix(s, minus) {
		return s
	}
	return "+" + s
}

// USDWholeSigned is a whole-dollar gain or flow: "+$4,222", "−$350".
func USDWholeSigned(v float64) string {
	s := USDWhole(v)
	if strings.HasPrefix(s, minus) {
		return s
	}
	return "+" + s
}

// USDShort is a chart axis or a tight label: "$950", "$1.2k", "$12k", "$1.4M".
func USDShort(v float64) string {
	a := math.Abs(v)
	var s string
	// Round half away from zero before printing: printf-style formatting
	// rounds a tie (an axis tick at 1,250) to even, and JS toFixed does not.
	tenths := func(x float64) string { return trimZero(strconv.FormatFloat(math.Round(x*10)/10, 'f', 1, 64)) }
	switch {
	case math.Round(a/1e5) >= 10:
		s = tenths(a/1e6) + "M"
	case math.Round(a/1e3) >= 10:
		s = Commas(int64(math.Round(a/1e3))) + "k"
	case math.Round(a) >= 1000:
		s = tenths(a/1e3) + "k"
	default:
		s = strconv.FormatInt(int64(math.Round(a)), 10)
	}
	return sign(v, s)
}

// Price is a per-share price or a fill, which is not a balance: always cents
// ("$432.10", where USD would say "$432"), four decimals below a cent (a
// 409A's $0.0015, which two decimals render "$0.00").
func Price(v float64) string {
	a := math.Abs(v)
	digits := 2
	if a > 0 && a < 0.01 {
		digits = 4
	}
	whole, frac, _ := strings.Cut(strconv.FormatFloat(a, 'f', digits, 64), ".")
	n, _ := strconv.ParseInt(whole, 10, 64)
	return sign(v, Commas(n)+"."+frac)
}

// Commas is a count with thousands separators: "22,635", "−1,204".
func Commas(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		return minus + s
	}
	return s
}

func sign(v float64, digits string) string {
	if v < 0 && strings.Trim(digits, "0.,") != "" {
		return minus + "$" + digits
	}
	return "$" + digits
}

func trimZero(s string) string { return strings.TrimSuffix(s, ".0") }

// Truncate keeps the first n characters of s and marks the cut with "…".
// It counts runes, not bytes: the six copies it replaced sliced s[:n] and
// could split a multi-byte character (an em dash, an emoji) into invalid
// UTF-8 in a push line or a log.
func Truncate(s string, n int) string {
	if n < 0 {
		n = 0
	}
	if len(s) <= n { // bytes ≥ runes, so this is a cheap exact "fits"
		return s
	}
	i := 0
	for pos := range s {
		if i == n {
			return s[:pos] + "…"
		}
		i++
	}
	return s
}

// Tail keeps the last n characters of s (runes, as Truncate), unmarked:
// the end of a command's output, where the error usually is.
func Tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}
