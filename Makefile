# See CLAUDE.md. `make check` is the gate.
.PHONY: check test pylint build webcheck generate hub-install hub-restart ship ota install-phone

check: test pylint build app-build-sim mac-build
	@echo "check: OK"

# ops/*.py parse, open life.db read-only, verify TLS, take the hub address,
# Eastern time and tokens from ops/hublib.py (reads files only).
pylint:
	ops/py.sh py-lint.py

test:
	cd hub && gofmt -l . | (! grep .) && go vet ./... && go test -timeout 180s ./...

build:
	cd hub && go build -o ../ops/bin/hub ./cmd/hub && go build -o ../ops/bin/lifectl ./cmd/lifectl
	$(call sign,ops/bin/hub,com.life.hub)

# Signed with your Apple Development certificate and pinned to identifier +
# team when both are set (TEAM_ID=… make build), so a macOS privacy grant
# survives a rebuild; otherwise the build keeps an ad-hoc signature.
TEAM_ID ?=
SIGN_ID ?= $(shell security find-identity -v -p codesigning 2>/dev/null | grep -m1 -o '"Apple Development: [^"]*"' | tr -d '"')
define sign
$(if $(and $(SIGN_ID),$(TEAM_ID)),codesign --force --sign "$(SIGN_ID)" --identifier $(2) --requirements '=designated => identifier "$(2)" and anchor apple generic and certificate leaf[subject.OU] = "$(TEAM_ID)"' $(1),codesign --force --sign - --identifier $(2) $(1))
endef

# Syntax-check the laptop console's JS. Deliberately NOT part of `check`: the
# hub is one static binary with no node toolchain and the gate must not grow a
# JS lane — but the console is served unbuilt, so a typo blanks the page
# instead of failing a build. Run it after editing web/, before a hub restart.
webcheck:
	chmod +x ops/webcheck.sh
	ops/webcheck.sh

# The other half of the same problem: parsing is not booting. Renders the live
# console in headless Chrome and fails if app.js threw before drawing.
web-smoke:
	chmod +x ops/web-smoke.sh
	ops/web-smoke.sh

# Screenshots of the same two views (ops/logs/web/), for looking at a change.
# ROUTE='#/calendar' shoots a third view (ops/logs/web/view.png). THREAD is
# quoted: unquoted and empty it collapsed, so ROUTE slid into the thread-id
# slot and view.png was never written — the shot a session asked for silently
# did not happen (2026-09-09).
web-shot:
	chmod +x ops/web-smoke.sh
	ops/web-smoke.sh shot '$(THREAD)' '$(ROUTE)'

# The same console drawn against FIXTURES (ops/web-preview.html) instead of
# the live hub, so a state your data does not happen to be in right now — a
# pending approval, a goal being edited — can still be looked at without
# creating a fake proposal on your board. ROUTE defaults to Sessions.
web-preview:
	chmod +x ops/web-smoke.sh
	ops/web-smoke.sh preview '$(or $(ROUTE),#/sessions)' $(OPEN)

# WALK the console instead of photographing it: click, hover,
# type, scroll, screenshot each step, and collect the page's own console
# errors. STEPS='goto #/money; click text=Checking; shot account' or
# FLOW=ops/flows/money.txt. Output: ops/logs/web/browse/NN-<name>.png.
# SIZE=720x900 draws it in a narrow window (a half-screen layout is looked at
# before it ships). NODE is nvm's node (no PATH assumptions from a session's shell).
NODE ?= $(shell command -v node || ls $$HOME/.nvm/versions/node/*/bin/node 2>/dev/null | tail -1)
browse:
	$(NODE) ops/browse.js $(if $(FLOW),--file $(FLOW),) $(if $(OUT),--out $(OUT),) $(if $(SIZE),--size $(SIZE),) $(BROWSE_FLAGS) '$(STEPS)'

# The contract is real hub output: TestFixtures (hub/internal/server) writes
# every GET the app decodes to shared/fixtures/, and the app's LifeTests
# (FixtureDecodeTests) decode them and fail on a key the Swift type lacks.
# `make test` fails when a fixture is stale; this rewrites them.
generate:
	cd hub && UPDATE_FIXTURES=1 go test ./internal/server -run TestFixtures -count=1

hub-install:
	ops/hub.sh install

hub-restart:
	ops/hub.sh restart

app-project:
	# Bundle id, app group and team come from app/Identity.xcconfig, overridden
	# by the git-ignored app/local.xcconfig setup writes (optional).
	cd app && xcodegen generate

app-build-sim:
	cd app && xcodebuild -project Life.xcodeproj -scheme Life -destination 'generic/platform=iOS Simulator' -derivedDataPath build CODE_SIGNING_ALLOWED=NO build -quiet

# Unit tests (app/LifeTests: decode every hub response type from api.md JSON).
# Needs a simulator, so it is not in `check`. SIM=<name> to pick another device.
# -only-testing keeps the UI driver (LifeUITests, minutes) out of this lane.
SIM ?= iPhone 17
app-test:
	cd app && xcodebuild -project Life.xcodeproj -scheme Life -destination 'platform=iOS Simulator,name=$(SIM)' -derivedDataPath build -only-testing:LifeTests CODE_SIGNING_ALLOWED=NO test -quiet

# WALK the phone app: tap, type, swipe, screenshot each step.
# STEPS='tab more; tap Configuration; shot config; tap Goals; shot goals' or
# FLOW=ops/flows/<name>.txt. SIM=/APPEARANCE= pick device and light/dark.
# Output: ops/logs/screens/ui/.
app-ui:
	bash ops/app-ui.sh

install-phone:
	ops/install-phone.sh

# The desktop app (LifeMac, Mac Catalyst over the same sources): build, sign,
# install into /Applications and relaunch. `make mac-build` only compiles it.
mac:
	bash ops/install-mac.sh

# Screenshot every desktop page (ops/logs/screens/mac/); PAGES='recs thread:<id>'.
mac-screens:
	bash ops/mac-screens.sh $(PAGES)

# Can the build `make mac` just made keep the decider code? A throwaway
# Keychain item, saved as Settings saves the code; -34018 = it cannot.
mac-keychain:
	bash ops/install-mac.sh probe

mac-build:
	cd app && xcodebuild -project Life.xcodeproj -scheme LifeMac -destination 'platform=macOS,variant=Mac Catalyst' -derivedDataPath build-mac CODE_SIGNING_ALLOWED=NO build -quiet

# Screenshot every tab in the simulator against the live hub (ops/logs/screens/).
screens: app-build-sim
	bash ops/screens.sh $(TABS)

# The same screens as iOS draws them after sunset, into dark-<tab>.png. Worth
# its own target: the screenshot harness pinned the simulator to light and had
# no switch, so no pass over this app had ever looked at dark mode.
screens-dark: app-build-sim
	APPEARANCE=dark bash ops/screens.sh $(TABS)

# ship = THE command after app/ changes (paid tier, 2026-08-22): ad-hoc
# archive + export, .ipa + manifest served by the hub at /ota/<token>/, you
# install from Safari anywhere on the tailnet (no LAN, no cable). Then raise
# a physical ask with the link (data/ota/url). LAN fallback: install-phone.
ship ota:
	chmod +x ops/ota.sh
	ops/ota.sh