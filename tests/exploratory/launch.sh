#!/usr/bin/env bash
set -euo pipefail

# Exploratory test launcher for sd CLI
# Builds the binary, sets up isolated environments, and deploys test agents via ntm
#
# Usage:
#   ./launch.sh           # Build + setup only, prints instructions
#   ./launch.sh --run     # Build + setup + spawn agents + send prompts

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
DATE="$(date +%Y-%m-%d)"
RUN_DIR="$SCRIPT_DIR/runs/$DATE"
BUILD_DIR="/tmp/sd-test"
EXPLORATORY_BASE="/tmp/sd-exploratory"
SESSION_LABEL="test-${DATE}"
SESSION_NAME="secure-dev--${SESSION_LABEL}"

# Agent definitions: index maps to pane number (pane = index + 1)
AGENTS=("cli-surface" "json-contract" "config-flags")
AGENT_COUNT=${#AGENTS[@]}

echo "=== sd Exploratory Test Launcher ==="
echo "Project: $PROJECT_DIR"
echo "Date:    $DATE"
echo ""

# --- Step 1: Build the sd binary ---
echo "[1/5] Building sd binary..."
mkdir -p "$BUILD_DIR"
(cd "$PROJECT_DIR" && go build -o "$BUILD_DIR/sd" ./cmd/sd/)
echo "  Built: $BUILD_DIR/sd"
echo "  Version: $($BUILD_DIR/sd version 2>/dev/null || echo 'unknown')"
echo ""

# --- Step 2: Set up isolated SD_HOME directories ---
echo "[2/5] Setting up agent environments..."
for agent in "${AGENTS[@]}"; do
    agent_home="$EXPLORATORY_BASE/$agent"
    rm -rf "$agent_home"
    mkdir -p "$agent_home"
    echo "  $agent -> $agent_home"
done
echo ""

# --- Step 3: Create run output directories ---
echo "[3/5] Creating report directories..."
for agent in "${AGENTS[@]}"; do
    mkdir -p "$RUN_DIR/$agent"
    echo "  $RUN_DIR/$agent/"
done
echo ""

# --- Step 4: Generate per-agent prompt files with environment context ---
echo "[4/5] Generating agent prompts with environment context..."
PROMPT_DIR="/tmp/sd-test-prompts"
rm -rf "$PROMPT_DIR"
mkdir -p "$PROMPT_DIR"

for i in "${!AGENTS[@]}"; do
    agent="${AGENTS[$i]}"
    prompt_file="$PROMPT_DIR/$agent.md"
    agent_home="$EXPLORATORY_BASE/$agent"

    cat > "$prompt_file" << PROMPT_EOF
$(cat "$SCRIPT_DIR/agents/$agent.md")

---

## Environment For This Run

- **SD_TEST_BIN**: $BUILD_DIR/sd
- **SD_TEST_HOME**: $agent_home
- **Report to**: $RUN_DIR/$agent/report.md
- **Project dir**: $PROJECT_DIR

**IMPORTANT**: Always run sd commands as:
\`\`\`
SD_HOME=$agent_home $BUILD_DIR/sd <args>
\`\`\`

Start now. Work through the phases systematically. Write your report when done.
PROMPT_EOF

    echo "  $agent -> $prompt_file ($(wc -l < "$prompt_file") lines)"
done
echo ""

# --- Step 5: Instructions ---
echo "[5/5] Ready."
echo ""
echo "To launch manually:"
echo ""
echo "  # 1. Spawn agents (creates panes, no prompts yet)"
echo "  ntm spawn secure-dev --label $SESSION_LABEL --cc=$AGENT_COUNT --no-user"
echo ""
echo "  # 2. Send each agent its mission"
for i in "${!AGENTS[@]}"; do
    agent="${AGENTS[$i]}"
    pane=$((i + 1))
    echo "  ntm send "$SESSION_NAME" --pane=$pane --file $PROMPT_DIR/$agent.md"
done
echo ""
echo "  # 3. Monitor"
echo "  ntm activity "$SESSION_NAME" --watch"
echo "  ntm watch "$SESSION_NAME""
echo ""
echo "  # 4. Collect results"
echo "  ntm summary "$SESSION_NAME" --format markdown"
echo ""

# Auto-launch if --run is passed
if [[ "${1:-}" == "--run" ]]; then
    echo "=== Launching ==="
    echo ""

    echo "Spawning $AGENT_COUNT agents..."
    ntm spawn secure-dev --label "$SESSION_LABEL" --cc="$AGENT_COUNT" --no-user --stagger-mode=smart
    echo ""

    # Wait for panes to be ready
    echo "Waiting for agents to initialize..."
    sleep 10

    echo "Sending agent missions..."
    for i in "${!AGENTS[@]}"; do
        agent="${AGENTS[$i]}"
        pane=$((i + 1))
        echo "  Sending to pane $pane: $agent"
        ntm send "$SESSION_NAME" --pane="$pane" --file "$PROMPT_DIR/$agent.md"
        sleep 5  # stagger to avoid rate limits
    done
    echo ""

    echo "=== Agents deployed ==="
    echo "Monitor: ntm activity "$SESSION_NAME" --watch"
    echo "Watch:   ntm watch "$SESSION_NAME""
fi
