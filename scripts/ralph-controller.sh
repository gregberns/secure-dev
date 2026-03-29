#!/bin/bash
# Ralph Loop Controller for sd rig
#
# Dispatches one bead at a time to fresh polecats.
# Loop is OUTSIDE the agent — each polecat gets fresh context.
#
# Usage:
#   ./ralph-controller.sh [max-iterations] [phase-epic]
#
# Examples:
#   ./ralph-controller.sh              # Run until no ready beads (max 20)
#   ./ralph-controller.sh 10           # Max 10 iterations
#   ./ralph-controller.sh 10 sd-ee7.1  # Only beads under phase 1 epic

set -euo pipefail

MAX_ITERATIONS=${1:-20}
PHASE_EPIC=${2:-""}
RIG="sd"
ITERATION=0
FAILURES=0
MAX_CONSECUTIVE_FAILURES=3

echo "═══════════════════════════════════════════"
echo " Ralph Loop Controller"
echo " Rig: $RIG"
echo " Max iterations: $MAX_ITERATIONS"
echo " Phase filter: ${PHASE_EPIC:-none (all ready beads)}"
echo "═══════════════════════════════════════════"

while [ $ITERATION -lt $MAX_ITERATIONS ]; do
  # Check for ready beads
  if [ -n "$PHASE_EPIC" ]; then
    READY_BEAD=$(cd /Users/gb/gt/sd && bd ready --parent "$PHASE_EPIC" --json 2>/dev/null | head -1 | jq -r '.id // empty' 2>/dev/null || true)
  else
    READY_BEAD=$(cd /Users/gb/gt/sd && bd ready --json 2>/dev/null | head -1 | jq -r '.id // empty' 2>/dev/null || true)
  fi

  if [ -z "$READY_BEAD" ]; then
    echo ""
    echo "✓ No ready beads remaining. Done after $ITERATION iterations."
    break
  fi

  ITERATION=$((ITERATION + 1))
  echo ""
  echo "━━━ Iteration $ITERATION/$MAX_ITERATIONS ━━━"
  echo "  Bead: $READY_BEAD"
  echo "  Dispatching to $RIG..."

  # Sling the bead — spawns a fresh polecat
  if gt sling "$READY_BEAD" "$RIG" --formula ralph-loop 2>&1; then
    FAILURES=0
    echo "  ✓ Polecat completed"
  else
    FAILURES=$((FAILURES + 1))
    echo "  ✗ Polecat failed (consecutive failures: $FAILURES)"

    if [ $FAILURES -ge $MAX_CONSECUTIVE_FAILURES ]; then
      echo ""
      echo "✗ Circuit breaker: $MAX_CONSECUTIVE_FAILURES consecutive failures."
      echo "  Last bead: $READY_BEAD"
      echo "  Halting. Investigate and retry manually."
      exit 1
    fi
  fi

  # Brief pause between iterations
  sleep 5
done

if [ $ITERATION -ge $MAX_ITERATIONS ]; then
  echo ""
  echo "⚠ Hit max iterations ($MAX_ITERATIONS). Beads may remain."
  echo "  Run again to continue, or check: bd ready"
fi

echo ""
echo "═══════════════════════════════════════════"
echo " Ralph Loop Complete"
echo " Iterations: $ITERATION"
echo " Remaining: $(cd /Users/gb/gt/sd && bd ready --count 2>/dev/null || echo '?')"
echo "═══════════════════════════════════════════"
