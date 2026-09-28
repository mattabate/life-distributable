"""The one shared library for the Python tools in ops/.

Every script that talks to the hub, reads life.db, needs "today", prices a
model, or calls GitHub / PostHog / Google imports it from here, so a fact
lives in one place:

  hub address   ops/hub.json `public_host` — the ONLY place it is written
                (lifectl and ops/hub.sh read the same key). Moving the hub to
                another machine is that one line (+ its cert files).
  hub auth      ops/secrets/hub.token, Bearer; TLS VERIFIED (the ts.net cert
                is publicly trusted — no CERT_NONE anywhere).
  life.db       read-only (`mode=ro`); writes go through the hub.
  Eastern time  ET / now_et() / today_et() — never a fixed -4h offset.
  prices        hub/internal/spend/prices.json, the table the hub embeds.

    import hublib
    hublib.call("GET", "/api/v1/goals")          # JSON in, JSON out
    hublib.today_et()                            # date in America/New_York
    hublib.db().execute("select …")              # read-only life.db
    hublib.gh("GET", "/repos/o/r")               # (status, payload)

Not a script: ops/py.sh runs it only by mistake (it prints this).
"""
import datetime as _dt
import json
import os
import sqlite3
import ssl
import sys
import urllib.error
import urllib.parse
import urllib.request
from zoneinfo import ZoneInfo

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))  # this checkout
ROOT = REPO  # data/ and ops/secrets/ live inside the checkout
SECRETS = ROOT + "/ops/secrets"
_cfg = None


# --- the hub -----------------------------------------------------------------

def config():
    """ops/hub.json, parsed once."""
    global _cfg
    if _cfg is None:
        with open(REPO + "/ops/hub.json") as f:
            _cfg = json.load(f)
    return _cfg


def host():
    """host:port the hub answers on, e.g. my-mac.example.ts.net:8443."""
    return config()["public_host"]


def url(path=""):
    return "https://" + host() + path


def token():
    with open(os.path.expanduser(config().get("token_file", SECRETS + "/hub.token"))) as f:
        return f.read().strip()


def tls():
    """A verifying TLS context (the ts.net certificate chains to a public CA)."""
    return ssl.create_default_context()


def request(method, path, data=None, headers=None, timeout=30):
    """One hub call with raw bytes in; returns (status, body bytes). Never raises on HTTP errors."""
    h = {"Authorization": "Bearer " + token()}
    h.update(headers or {})
    req = urllib.request.Request(url(path), data=data, method=method, headers=h)
    try:
        with urllib.request.urlopen(req, timeout=timeout, context=tls()) as r:
            return r.status, r.read()
    except urllib.error.HTTPError as e:
        return e.code, e.read()


def call(method, path, body=None, timeout=30):
    """One hub call, JSON in and JSON out; an HTTP error exits with the hub's words."""
    data = json.dumps(body).encode() if body is not None else None
    st, out = request(method, path, data, {"Content-Type": "application/json"}, timeout)
    if st >= 300:
        raise SystemExit("hub %d: %s" % (st, out.decode("utf-8", "replace").strip()[:400]))
    return json.loads(out.decode() or "null")


# --- time: Eastern, DST-correct ---------------------------------------------

ET = ZoneInfo("America/New_York")


def now_et():
    return _dt.datetime.now(ET)


def today_et():
    return now_et().date()


def et(ts):
    """An ISO timestamp (or datetime) as an aware Eastern datetime."""
    if isinstance(ts, str):
        ts = _dt.datetime.fromisoformat(ts.replace("Z", "+00:00"))
    if ts.tzinfo is None:
        ts = ts.replace(tzinfo=_dt.timezone.utc)
    return ts.astimezone(ET)


# --- life.db, read-only -----------------------------------------------------

def db_path():
    return os.path.expanduser(config().get("db_path", ROOT + "/data/life.db"))


def db():
    """A READ-ONLY handle on life.db; the hub is the only writer."""
    return sqlite3.connect("file:" + db_path() + "?mode=ro", uri=True)


# --- secrets env files --------------------------------------------------------

def env(name):
    """KEY=VALUE pairs from ops/secrets/<name>.env (quotes stripped). Missing file = {}."""
    out = {}
    try:
        lines = open(SECRETS + "/" + name + ".env").read().splitlines()
    except FileNotFoundError:
        return out
    for line in lines:
        line = line.strip()
        if line.startswith("export "):
            line = line[7:]
        if line and not line.startswith("#") and "=" in line:
            k, v = line.split("=", 1)
            out[k.strip()] = v.strip().strip('"').strip("'")
    return out


# --- model prices (the hub's own table) ---------------------------------------

_prices = None


def _price_table():
    global _prices
    if _prices is None:
        with open(REPO + "/hub/internal/spend/prices.json") as f:
            _prices = json.load(f)
    return _prices


def price(model):
    """(input, output, cache_read) $/MTok for a model id, longest prefix first."""
    t = _price_table()
    for m in t["models"]:
        if model.startswith(m["prefix"]):
            p = m
            break
    else:
        p = t["unknown"]
    cr = p.get("cache_read") or p["input"] * t["cache"]["read"]
    return p["input"], p["output"], cr


def cost(model, inp=0, out=0, cache_read=0, write_5m=0, write_1h=0):
    """USD for one request's usage, exactly as hub/internal/spend prices it."""
    i, o, cr = price(model)
    c = _price_table()["cache"]
    return (inp * i + cache_read * cr + write_5m * i * c["write_5m"]
            + write_1h * i * c["write_1h"] + out * o) / 1e6


# --- GitHub --------------------------------------------------------------------

GH_API = "https://api.github.com"


def gh_token():
    with open(SECRETS + "/github.token") as f:
        return f.read().strip()


def gh(method, path, body=None, accept=None, timeout=60, raw=False):
    """One GitHub REST call with the PAT; returns (status, payload), never raises on HTTP errors.

    `path` is /repos/… (or a full https://api.github.com URL, e.g. a Link
    header's next page). payload is parsed JSON, or text when raw=True or
    the answer is not JSON.
    """
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(path if path.startswith("https://") else GH_API + path,
                                 data=data, method=method)
    req.add_header("Authorization", "Bearer " + gh_token())
    req.add_header("Accept", accept or "application/vnd.github+json")
    req.add_header("X-GitHub-Api-Version", "2022-11-28")
    if data is not None:
        req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            st, text = r.status, r.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        st, text = e.code, e.read().decode("utf-8", "replace")
    if raw:
        return st, text
    try:
        return st, json.loads(text or "null")
    except ValueError:
        return st, text


def gh_ok(method, path, body=None, what=None, **kw):
    """gh() that exits on a non-2xx answer and returns just the payload."""
    st, out = gh(method, path, body, **kw)
    if st >= 300:
        sys.exit("%s: GitHub %d %s" % (what or path, st, json.dumps(out)[:400]))
    return out


def gh_all(path, per_page=100):
    """Every item of a paginated GitHub list (follows ?page= until a short page)."""
    out, page = [], 1
    sep = "&" if "?" in path else "?"
    while True:
        st, items = gh("GET", "%s%sper_page=%d&page=%d" % (path, sep, per_page, page))
        if st != 200:
            sys.exit("%s: GitHub %d %s" % (path, st, json.dumps(items)[:300]))
        if isinstance(items, dict):  # search / actions answers wrap the list
            items = next((v for v in items.values() if isinstance(v, list)), [])
        out.extend(items)
        if len(items) < per_page:
            return out
        page += 1


# --- PostHog (HogQL, key in audience.env) --------------------------------------

def posthog(project, sql, timeout=60, extra=None):
    """Run one HogQL query against a PostHog project; returns the parsed answer.

    Key POSTHOG_API_KEY (query:read) and POSTHOG_HOST from audience.env; the key
    is never printed. An HTTP error exits with PostHog's words.
    """
    e = env("audience")
    key = e.get("POSTHOG_API_KEY")
    if not key:
        sys.exit("no POSTHOG_API_KEY in ops/secrets/audience.env")
    body = {"query": {"kind": "HogQLQuery", "query": sql}}
    body.update(extra or {})
    req = urllib.request.Request(
        "%s/api/projects/%s/query/" % (e.get("POSTHOG_HOST", "https://us.posthog.com"), project),
        data=json.dumps(body).encode(), method="POST",
        headers={"Authorization": "Bearer " + key, "Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return json.load(r)
    except urllib.error.HTTPError as err:
        sys.exit("posthog %d: %s" % (err.code, err.read().decode("utf-8", "replace")[:400]))


def posthog_get(path, timeout=60):
    """GET a PostHog REST path (e.g. /api/projects/123/query/<id>/) with the same key."""
    e = env("audience")
    req = urllib.request.Request(e.get("POSTHOG_HOST", "https://us.posthog.com") + path,
                                 headers={"Authorization": "Bearer " + e.get("POSTHOG_API_KEY", "")})
    with urllib.request.urlopen(req, timeout=timeout) as r:
        return json.load(r)


# --- Google (the hub's refresh tokens, google.env) -----------------------------

def google_token(account="main"):
    """A fresh access token for GOOGLE_TOKEN_<account> (the scopes the hub already holds)."""
    e = env("google")
    rt = e.get("GOOGLE_TOKEN_" + account)
    if not rt:
        sys.exit("no GOOGLE_TOKEN_%s in ops/secrets/google.env" % account)
    form = urllib.parse.urlencode({
        "refresh_token": rt, "client_id": e["GOOGLE_CLIENT_ID"],
        "client_secret": e["GOOGLE_CLIENT_SECRET"], "grant_type": "refresh_token",
    }).encode()
    with urllib.request.urlopen(urllib.request.Request(
            "https://oauth2.googleapis.com/token", data=form), timeout=30) as r:
        return json.load(r)["access_token"]


def google_get(u, tok, timeout=60):
    """(status, JSON) for one authorized Google REST GET; never raises on HTTP errors."""
    req = urllib.request.Request(u, headers={"Authorization": "Bearer " + tok})
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return r.status, json.load(r)
    except urllib.error.HTTPError as err:
        return err.code, {"raw": err.read().decode("utf-8", "replace")[:400]}


if __name__ == "__main__":
    print(__doc__)
