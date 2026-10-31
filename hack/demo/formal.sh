#!/bin/sh
# Builds a throwaway Lean project and shows paranoid's formal checks.
#
# The scenario: a tiny Lean project with one honest theorem and one
# locked spec. An "agent" commit then cheats four ways: it adds a
# sorry, adds an axiom, weakens the locked theorem, and turns on a
# checker soundness switch. paranoid catches all of them with static
# rules only, so no Lean install, no container, and no network are
# needed.
#
# Usage, from the repository root:
#
#   sh hack/demo/formal.sh
#
# The script builds paranoid into a temp directory, works only inside
# another temp directory, and deletes both when it ends. It never
# touches your repository.
set -eu

ROOT="$(pwd)"
if [ ! -f "$ROOT/cmd/paranoid/main.go" ]; then
    echo "run this from the paranoid repository root" >&2
    exit 1
fi

WORK="$(mktemp -d "${TMPDIR:-/tmp}/paranoid-formal-demo.XXXXXX")"
cleanup() {
    rm -rf "$WORK"
}
trap cleanup EXIT INT TERM

BIN="$WORK/paranoid"
echo "building paranoid"
CGO_ENABLED=0 go build -o "$BIN" ./cmd/paranoid

DEMO_DIR="$WORK/repo"
mkdir "$DEMO_DIR"
echo "demo repository: $DEMO_DIR"
cd "$DEMO_DIR"

git init -q --initial-branch=main
git config user.name "demo"
git config user.email "demo@example.com"
git config commit.gpgsign false

# ---- The honest baseline ------------------------------------------------
cat > lakefile.toml <<'TOML'
name = "demo"
defaultTargets = ["Demo"]

[[lean_lib]]
name = "Demo"
TOML

cat > lean-toolchain <<'TXT'
leanprover/lean4:v4.24.0
TXT

cat > Demo.lean <<'LEAN'
theorem add_zero_right (n : Nat) : n + 0 = n := by
  simp

theorem double_eq (n : Nat) : n + n = 2 * n := by
  omega
LEAN

git add -A
git commit -q -m "baseline: two proofs"

# A human approves the statement of double_eq and locks it.
echo
echo "$ paranoid formal lock double_eq"
"$BIN" formal lock double_eq
git add -A
git commit -q -m "lock the double_eq spec"
BASE="$(git rev-parse HEAD)"

# ---- The agent's commit -------------------------------------------------
# The agent could not finish a proof, so it cheats: it weakens the
# locked theorem into something trivial, leaves a sorry, adds an axiom,
# and tells Lean to skip the kernel check.
cat > Demo.lean <<'LEAN'
set_option debug.skipKernelTC true

axiom magic : forall n : Nat, n * 0 = 1

theorem add_zero_right (n : Nat) : n + 0 = n := by
  sorry

theorem double_eq (n : Nat) : n + n = n + n := by
  rfl
LEAN

git add -A
git commit -q -m "finish the proofs"

echo
echo "$ paranoid formal status"
"$BIN" formal status || true

echo
echo "$ paranoid verify --base $BASE --sandbox none --offline"
echo
"$BIN" verify --base "$BASE" --head HEAD --sandbox none --offline || true
