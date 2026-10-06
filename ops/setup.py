"""Write this Mac's config files from what setup knows (SETUP.md steps 5 and 7).

    ops/py.sh setup.py hub --owner Sam --usage no [--surfaces phone,desktop,web]
                                                       # ops/hub.json
    ops/py.sh setup.py app --github samsmith [--team ABCDE12345]
                                                       # app/local.xcconfig + ops/app.env
    ops/py.sh setup.py apns --key-id K1234ABCDE --team ABCDE12345 --key-file ~/Downloads/AuthKey_K1234ABCDE.p8
    ops/py.sh setup.py backup --bucket sam-life-backup # ops/secrets/restic.env (no keys yet)
    ops/py.sh setup.py show                            # what is configured, what is missing

`hub` reads the tailnet name and IP from the Tailscale CLI and the claude
binary from PATH, and refuses to overwrite an existing ops/hub.json without
--force. Every file it writes is git-ignored; secrets go to ops/secrets/
(mode 600) and are never printed.
"""
import argparse
import json
import os
import shutil
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
HUB_JSON = os.path.join(ROOT, "ops", "hub.json")
EXAMPLE = os.path.join(ROOT, "ops", "hub.json.example")
LOCAL_XC = os.path.join(ROOT, "app", "local.xcconfig")
APP_ENV = os.path.join(ROOT, "ops", "app.env")
SECRETS = os.path.join(ROOT, "ops", "secrets")
TAILSCALE = ["tailscale", "/Applications/Tailscale.app/Contents/MacOS/Tailscale"]


def tailscale_self():
    """(dns name without the trailing dot, first IPv4) of this Mac on the tailnet."""
    for exe in TAILSCALE:
        if shutil.which(exe) or os.path.exists(exe):
            r = subprocess.run([exe, "status", "--json"], capture_output=True, text=True)
            if r.returncode == 0:
                me = json.loads(r.stdout).get("Self", {})
                ips = [ip for ip in me.get("TailscaleIPs", []) if "." in ip]
                return me.get("DNSName", "").rstrip("."), (ips[0] if ips else "")
    sys.exit("setup: Tailscale is not running (install the Mac app, sign in, then retry)")


def cmd_hub(a):
    if os.path.exists(HUB_JSON) and not a.force:
        sys.exit("setup: ops/hub.json exists (use --force to rewrite it)")
    host, ip = tailscale_self()
    if not host or not ip:
        sys.exit("setup: Tailscale gave no DNS name / IPv4 — enable MagicDNS in the admin console")
    claude = shutil.which("claude")
    if not claude:
        sys.exit("setup: `claude` is not on PATH — install Claude Code first")
    cfg = json.load(open(EXAMPLE))
    cfg.pop("_comment", None)
    cfg["owner_name"] = a.owner
    cfg["usage_opt_in"] = a.usage == "yes"
    cfg["surfaces"] = a.surfaces
    cfg["listen_addr"] = "%s:%d" % (ip, a.port)
    cfg["public_host"] = "%s:%d" % (host, a.port)
    cfg["cert_file"] = "~/life/ops/secrets/%s.crt" % host
    cfg["key_file"] = "~/life/ops/secrets/%s.key" % host
    cfg["claude_bin"] = os.path.realpath(claude) if a.resolve else claude
    for k in ("apns_key_id", "apns_team_id", "apns_bundle_id"):
        cfg[k] = ""
    cfg["apns_key_file"] = ""
    os.makedirs(SECRETS, mode=0o700, exist_ok=True)
    with open(HUB_JSON, "w") as f:
        json.dump(cfg, f, indent=2)
        f.write("\n")
    print("wrote ops/hub.json: owner %s, hub https://%s:%d, apps %s"
          % (a.owner, host, a.port, ", ".join(a.surfaces)))


SURFACES = ("phone", "desktop", "web")


def surfaces(v):
    """`--surfaces phone,web` → ["phone", "web"], in the fixed order."""
    picked = {s.strip().lower() for s in v.split(",") if s.strip()}
    bad = picked - set(SURFACES)
    if bad or not picked:
        raise argparse.ArgumentTypeError(
            "a comma list of %s (got %r)" % (", ".join(SURFACES), v))
    return [s for s in SURFACES if s in picked]


def cmd_app(a):
    bundle = "com.%s.life" % a.github.lower()
    lines = [
        "// Written by ops/setup.py (git-ignored). Overrides app/Identity.xcconfig.",
        "LIFE_BUNDLE_ID = " + bundle,
        "LIFE_APP_GROUP = group." + bundle,
        "DEVELOPMENT_TEAM = " + (a.team or ""),
        "",
    ]
    with open(LOCAL_XC, "w") as f:
        f.write("\n".join(lines))
    if not os.path.exists(APP_ENV):
        shutil.copy(os.path.join(ROOT, "ops", "app.env.example"), APP_ENV)
    if os.path.exists(HUB_JSON):
        cfg = json.load(open(HUB_JSON))
        cfg["apns_bundle_id"] = bundle
        if a.team:
            cfg["apns_team_id"] = a.team
        with open(HUB_JSON, "w") as f:
            json.dump(cfg, f, indent=2)
            f.write("\n")
    print("wrote app/local.xcconfig: bundle %s, team %s" % (bundle, a.team or "(none: simulator only)"))


def cmd_apns(a):
    src = os.path.expanduser(a.key_file)
    if not os.path.exists(src):
        sys.exit("setup: no such key file: " + a.key_file)
    os.makedirs(SECRETS, mode=0o700, exist_ok=True)
    dst = os.path.join(SECRETS, "apns.p8")
    shutil.copy(src, dst)
    os.chmod(dst, 0o600)
    cfg = json.load(open(HUB_JSON))
    cfg.update({"apns_key_file": "~/life/ops/secrets/apns.p8", "apns_key_id": a.key_id,
                "apns_team_id": a.team, "apns_production": True})
    with open(HUB_JSON, "w") as f:
        json.dump(cfg, f, indent=2)
        f.write("\n")
    print("APNs key stored in ops/secrets/apns.p8; hub.json updated (restart the hub: ops/hub.sh restart)")


def cmd_backup(a):
    dst = os.path.join(SECRETS, "restic.env")
    if os.path.exists(dst):
        sys.exit("setup: ops/secrets/restic.env exists — edit RESTIC_REPOSITORY in it by hand")
    text = open(os.path.join(ROOT, "ops", "restic.env.example")).read()
    text = text.replace("b2:<bucket-name>:life", "b2:%s:life" % a.bucket)
    os.makedirs(SECRETS, mode=0o700, exist_ok=True)
    fd = os.open(dst, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w") as f:
        f.write(text)
    print("wrote ops/secrets/restic.env for bucket %s; next: ops/b2-mint-keys.sh in Terminal" % a.bucket)


def cmd_show(_):
    def mark(ok, what):
        print(("  ok   " if ok else "  --   ") + what)
    cfg = json.load(open(HUB_JSON)) if os.path.exists(HUB_JSON) else {}
    mark(bool(cfg), "ops/hub.json")
    chosen = cfg.get("surfaces") or list(SURFACES)
    print("       apps: %s" % ", ".join(chosen))
    host = cfg.get("public_host", "").split(":")[0]
    mark(bool(host) and os.path.exists(os.path.join(SECRETS, host + ".crt")), "TLS certificate (ops/renew-cert.sh)")
    mark(os.path.exists(os.path.join(SECRETS, "hub.token")), "hub token (made on first hub start)")
    mark(os.path.exists(os.path.join(SECRETS, "restic.env")), "backup credentials ops/secrets/restic.env")
    if "phone" in chosen or "desktop" in chosen:
        mark(os.path.exists(LOCAL_XC), "app identity app/local.xcconfig")
    if "phone" in chosen:
        mark(bool(cfg.get("apns_key_file")), "push key (APNs)")
        mark(os.path.exists(os.path.join(ROOT, "data", "ota", "current.json")), "an app build published (make ship)")
    if "desktop" in chosen:
        mark(os.path.exists("/Applications/life.app"), "desktop app installed (make mac)")


def main():
    p = argparse.ArgumentParser(prog="ops/py.sh setup.py")
    sub = p.add_subparsers(dest="cmd", required=True)
    h = sub.add_parser("hub")
    h.add_argument("--owner", required=True, help="the name your agent calls you")
    h.add_argument("--usage", choices=["yes", "no"], required=True,
                   help="send the weekly anonymous usage heartbeat (SETUP.md step 5)")
    h.add_argument("--surfaces", type=surfaces, default=list(SURFACES),
                   help="which apps: comma list of phone,desktop,web (default all three)")
    h.add_argument("--port", type=int, default=8443)
    h.add_argument("--force", action="store_true")
    h.add_argument("--resolve", action="store_true", help="store claude's resolved real path")
    ap = sub.add_parser("app")
    ap.add_argument("--github", required=True, help="your GitHub username (bundle id com.<it>.life)")
    ap.add_argument("--team", default="", help="Apple Developer team id (10 chars)")
    k = sub.add_parser("apns")
    k.add_argument("--key-id", required=True)
    k.add_argument("--team", required=True)
    k.add_argument("--key-file", required=True)
    b = sub.add_parser("backup")
    b.add_argument("--bucket", required=True, help="the B2 bucket name")
    sub.add_parser("show")
    a = p.parse_args()
    {"hub": cmd_hub, "app": cmd_app, "apns": cmd_apns, "backup": cmd_backup,
     "show": cmd_show}[a.cmd](a)


if __name__ == "__main__":
    main()
