package main

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"time"
)

// certWarn: how long before the hub's TLS cert expires its ask is raised.
// The cert lasts ~90 days and ops/renew-cert.sh runs monthly, so a renewal
// that worked leaves 60+ days; 21 left means two runs have failed.
const certWarn = 21 * 24 * time.Hour

// certAsk reads the hub's TLS cert and, inside certWarn of its NotAfter,
// says what to ask the owner. The hub loads the cert once at start, so
// nothing else ever looked: a failed renewal was silent until TLS broke and
// the phone could not reach the hub (life-distributable#64). The title
// carries the date, so the ask is raised once per cert (AskSeen) and a
// renewed cert that fails again gets its own.
func certAsk(certFile string, now time.Time) (title, detail string, due bool, err error) {
	b, err := os.ReadFile(certFile)
	if err != nil {
		return "", "", false, err
	}
	blk, _ := pem.Decode(b)
	if blk == nil {
		return "", "", false, fmt.Errorf("%s: no PEM block", certFile)
	}
	c, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		return "", "", false, fmt.Errorf("%s: %w", certFile, err)
	}
	if c.NotAfter.Sub(now) > certWarn {
		return "", "", false, nil
	}
	day := c.NotAfter.Local().Format("Jan 2, 2006")
	title = "Renew the hub's TLS cert: it expires on " + day
	detail = fmt.Sprintf("Run `ops/renew-cert.sh` on the Mac the hub runs on; it mints a new cert and restarts the hub. Past %s the phone and the web cannot reach the hub.\n\nThe monthly renewal has not landed: see ops/logs/renew-cert.log.", c.NotAfter.Local().Format("Mon Jan 2 15:04"))
	return title, detail, true, nil
}
