package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeCert writes a self-signed PEM cert valid until notAfter.
func writeCert(t *testing.T, notAfter time.Time) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "hub.test"},
		NotBefore: notAfter.Add(-90 * 24 * time.Hour), NotAfter: notAfter}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "hub.crt")
	if err := os.WriteFile(p, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// The hub loads its cert once and nothing read NotAfter: a failed monthly
// ops/renew-cert.sh was silent until TLS broke and the phone could not reach
// the hub (life-distributable#64). Inside 21 days the daily check raises an
// ask whose title is the action and carries the date (so it is raised once
// per cert, deduped by title); a fresh cert raises nothing.
func TestCertCloseToExpiryIsAnAsk(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.Local)
	expires := now.Add(10 * 24 * time.Hour)
	title, detail, due, err := certAsk(writeCert(t, expires), now)
	if err != nil || !due {
		t.Fatalf("due=%v err=%v", due, err)
	}
	if !strings.HasPrefix(title, "Renew the hub's TLS cert") || !strings.Contains(title, expires.Format("Jan 2, 2006")) {
		t.Fatalf("title %q", title)
	}
	if !strings.Contains(detail, "ops/renew-cert.sh") {
		t.Fatalf("detail %q", detail)
	}
	if _, _, due, err := certAsk(writeCert(t, now.Add(60*24*time.Hour)), now); err != nil || due {
		t.Fatalf("a 60-day cert: due=%v err=%v", due, err)
	}
	if _, _, _, err := certAsk(filepath.Join(t.TempDir(), "missing.crt"), now); err == nil {
		t.Fatal("a missing cert file is no error")
	}
}

// A failed `tailscale cert` in ops/renew-cert.sh raises an ask through
// lifectl, as backup.sh's trap does, instead of one log line. Run on a copy
// with a failing tailscale and a lifectl that records its arguments.
func TestRenewCertFailureIsAnAsk(t *testing.T) {
	if _, err := exec.LookPath("plutil"); err != nil {
		t.Skip("renew-cert.sh reads hub.json with plutil (macOS)")
	}
	src, err := os.ReadFile("../../../ops/renew-cert.sh")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ops, fake := filepath.Join(dir, "ops"), filepath.Join(dir, "fake")
	asked := filepath.Join(dir, "asked")
	for _, d := range []string{filepath.Join(ops, "bin"), fake} {
		os.MkdirAll(d, 0o755)
	}
	os.WriteFile(filepath.Join(ops, "renew-cert.sh"), src, 0o755)
	os.WriteFile(filepath.Join(ops, "hub.json"), []byte(`{"public_host":"hub.test:8443"}`), 0o644)
	os.WriteFile(filepath.Join(ops, "bin", "lifectl"), []byte("#!/bin/sh\necho \"$@\" > "+asked+"\n"), 0o755)
	os.WriteFile(filepath.Join(fake, "tailscale"), []byte("#!/bin/sh\necho no >&2\nexit 3\n"), 0o755)
	cmd := exec.Command("bash", filepath.Join(ops, "renew-cert.sh"))
	cmd.Env = append(os.Environ(), "PATH="+fake+":"+os.Getenv("PATH"))
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("a failed renewal exited 0: %s", out)
	}
	b, err := os.ReadFile(asked)
	if err != nil || !strings.Contains(string(b), "ask add Renew the hub's TLS cert") || !strings.Contains(string(b), "exit 3") {
		t.Fatalf("no ask raised: %q %v", b, err)
	}
}
