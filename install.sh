#!/usr/bin/env bash
set -euo pipefail

# zoekt-vanzelf installer
# Installs zoekt + zoekt-vanzelf and sets up macOS launchd agents.

ZOEKT_PORT=6070
VANZELF_PORT=6071
ZOEKT_INDEX_DIR="$HOME/.zoekt"
SRC_ROOT="$HOME/src"
LAUNCH_AGENTS="$HOME/Library/LaunchAgents"

# Colors
bold="\033[1m"
dim="\033[90m"
green="\033[32m"
yellow="\033[33m"
red="\033[31m"
reset="\033[0m"

info()  { printf "${bold}==> %s${reset}\n" "$*"; }
ok()    { printf "${green}  ✓ %s${reset}\n" "$*"; }
warn()  { printf "${yellow}  ! %s${reset}\n" "$*"; }
error() { printf "${red}  ✗ %s${reset}\n" "$*"; exit 1; }

# --- Preflight checks ---

info "Checking prerequisites"

command -v go >/dev/null 2>&1 || error "Go is not installed. Install it from https://go.dev/dl/ or via: brew install go"
ok "Go $(go version | awk '{print $3}' | sed 's/go//')"

command -v git >/dev/null 2>&1 || error "git is not installed"
ok "git"

GOBIN="$(go env GOBIN)"
if [ -z "$GOBIN" ]; then
    GOBIN="$(go env GOPATH)/bin"
fi
ok "GOBIN: $GOBIN"

# Ensure GOBIN is in PATH for the rest of this script.
export PATH="$GOBIN:$PATH"

# Build the PATH that launchd agents will use.
# Include GOBIN + common system paths.
AGENT_PATH="$GOBIN:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

# --- Install zoekt ---

info "Installing zoekt"

if command -v zoekt-webserver >/dev/null 2>&1; then
    ok "zoekt-webserver already installed at $(command -v zoekt-webserver)"
else
    echo "  Installing zoekt-webserver..."
    go install github.com/sourcegraph/zoekt/cmd/zoekt-webserver@latest
    ok "zoekt-webserver installed"
fi

if command -v zoekt-git-index >/dev/null 2>&1; then
    ok "zoekt-git-index already installed at $(command -v zoekt-git-index)"
else
    echo "  Installing zoekt-git-index..."
    go install github.com/sourcegraph/zoekt/cmd/zoekt-git-index@latest
    ok "zoekt-git-index installed"
fi

# --- Install zoekt-vanzelf ---

info "Installing zoekt-vanzelf"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
if [ -f "$SCRIPT_DIR/go.mod" ] && grep -q "zoekt-vanzelf" "$SCRIPT_DIR/go.mod" 2>/dev/null; then
    echo "  Building from source in $SCRIPT_DIR..."
    (cd "$SCRIPT_DIR" && go install ./cmd/zoekt-vanzelf)
else
    echo "  Installing from module..."
    go install github.com/dvydra/zoekt-vanzelf/cmd/zoekt-vanzelf@latest
fi
ok "zoekt-vanzelf installed at $GOBIN/zoekt-vanzelf"

# --- Initial index ---

info "Setting up index directory"
mkdir -p "$ZOEKT_INDEX_DIR"
ok "$ZOEKT_INDEX_DIR"

if [ -d "$SRC_ROOT" ] && [ -z "$(ls -A "$ZOEKT_INDEX_DIR" 2>/dev/null | head -1)" ]; then
    info "Running initial index of repos under $SRC_ROOT"
    echo "  This may take a few minutes on first run..."

    # Find git repos and index them.
    find "$SRC_ROOT" -maxdepth 3 -name .git -type d 2>/dev/null | while read -r gitdir; do
        repo="$(dirname "$gitdir")"
        name="${repo#$SRC_ROOT/}"
        printf "  Indexing %s..." "$name"
        if zoekt-git-index -index "$ZOEKT_INDEX_DIR" "$repo" 2>/dev/null; then
            printf " ${green}done${reset}\n"
        else
            printf " ${yellow}skipped${reset}\n"
        fi
    done
    ok "Initial index complete"
elif [ ! -d "$SRC_ROOT" ]; then
    warn "No $SRC_ROOT directory found — skipping initial index"
    warn "Create it and run: zoekt-vanzelf reindex"
else
    ok "Index already exists, skipping initial index"
fi

# --- Set up launchd agents ---

info "Setting up launchd agents"
mkdir -p "$LAUNCH_AGENTS"

ZOEKT_WEBSERVER_BIN="$(command -v zoekt-webserver)"
ZOEKT_VANZELF_BIN="$(command -v zoekt-vanzelf)"

# Helper to write a plist, unload old version if present, and load the new one.
write_agent() {
    local label="$1"
    local plist_path="$LAUNCH_AGENTS/$label.plist"
    local content="$2"

    if launchctl list "$label" >/dev/null 2>&1; then
        launchctl bootout "gui/$(id -u)" "$plist_path" 2>/dev/null || true
    fi

    echo "$content" > "$plist_path"
    launchctl bootstrap "gui/$(id -u)" "$plist_path"
    ok "$label"
}

# --- zoekt-webserver ---
write_agent "com.zoekt.serve" "<?xml version=\"1.0\" encoding=\"UTF-8\"?>
<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://apple.com/DTDs/PropertyList-1.0.dtd\">
<plist version=\"1.0\">
<dict>
    <key>Label</key>
    <string>com.zoekt.serve</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>$AGENT_PATH</string>
    </dict>
    <key>ProgramArguments</key>
    <array>
        <string>$ZOEKT_WEBSERVER_BIN</string>
        <string>-index</string>
        <string>$ZOEKT_INDEX_DIR</string>
        <string>-listen</string>
        <string>:$ZOEKT_PORT</string>
        <string>-rpc</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/tmp/zoekt-serve.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/zoekt-serve.log</string>
</dict>
</plist>"

# --- zoekt-vanzelf ---
write_agent "com.zoekt.vanzelf" "<?xml version=\"1.0\" encoding=\"UTF-8\"?>
<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://apple.com/DTDs/PropertyList-1.0.dtd\">
<plist version=\"1.0\">
<dict>
    <key>Label</key>
    <string>com.zoekt.vanzelf</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>$AGENT_PATH</string>
    </dict>
    <key>ProgramArguments</key>
    <array>
        <string>$ZOEKT_VANZELF_BIN</string>
        <string>serve</string>
        <string>-port</string>
        <string>$VANZELF_PORT</string>
        <string>-zoekt</string>
        <string>http://localhost:$ZOEKT_PORT</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/tmp/zoekt-vanzelf.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/zoekt-vanzelf.log</string>
</dict>
</plist>"

# --- Done ---

echo ""
info "Installation complete"
echo ""
echo "  zoekt-webserver  → http://localhost:$ZOEKT_PORT  (base index)"
echo "  zoekt-vanzelf    → http://localhost:$VANZELF_PORT  (proxy with live working tree)"
echo ""
echo "  Logs:"
echo "    tail -f /tmp/zoekt-serve.log"
echo "    tail -f /tmp/zoekt-vanzelf.log"
echo ""
echo "  Verify:"
echo "    zoekt-vanzelf status"
echo ""
echo "  Optional: install neogrok for a web UI:"
echo "    npm install -g neogrok"
echo "    Then point it at http://localhost:$VANZELF_PORT"
