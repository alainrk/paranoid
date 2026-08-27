#!/bin/sh
# Builds the launch demo repository and runs paranoid on it.
#
# The scenario: a small Python project where an "agent" was asked to
# fix a division bug. The agent commit fixes it wrong, deletes the
# failing test file, skips another test, imports a package that does
# not exist, and writes a summary claiming all 14 tests pass.
#
# Usage, from the repository root:
#
#   make build
#   sh hack/demo/demo.sh
#
# With docker running, the clean room also catches the failing suite
# (SBX001) and the lying summary (CLM002).
set -eu

BIN="$(pwd)/bin/paranoid"
if [ ! -x "$BIN" ]; then
    echo "build the binary first: make build" >&2
    exit 1
fi

DEMO_DIR="$(mktemp -d "${TMPDIR:-/tmp}/paranoid-demo.XXXXXX")"
echo "demo repository: $DEMO_DIR"
cd "$DEMO_DIR"

git init -q --initial-branch=main
git config user.name "demo"
git config user.email "demo@example.com"
git config commit.gpgsign false

# ---- The honest baseline ------------------------------------------------
cat > calc.py <<'PY'
def add(a, b):
    return a + b


def div(a, b):
    return a / b
PY

cat > requirements.txt <<'PY'
pytest
PY

cat > test_addition.py <<'PY'
from calc import add


def test_add_small():
    assert add(1, 2) == 3


def test_add_zero():
    assert add(0, 7) == 7


def test_add_negative():
    assert add(-2, 2) == 0
PY

cat > test_division.py <<'PY'
import pytest

from calc import div


def test_div_exact():
    assert div(6, 3) == 2


def test_div_by_zero_raises():
    with pytest.raises(ZeroDivisionError):
        div(1, 0)
PY

git add -A
git commit -q -m "baseline: calculator with tests"
BASE="$(git rev-parse HEAD)"

# ---- The agent's commit -------------------------------------------------
# "Fixes" division by swallowing the error, deletes the failing test
# file, skips an addition test, and imports a package that does not
# exist on PyPI.
cat > calc.py <<'PY'
import requests_toolkit_pro


def add(a, b):
    return a + b


def div(a, b):
    if b == 0:
        return 0
    return a / b
PY

cat > requirements.txt <<'PY'
pytest
requests-toolkit-pro
PY

rm test_division.py

cat > test_addition.py <<'PY'
import pytest

from calc import add


def test_add_small():
    assert add(1, 2) == 3


@pytest.mark.skip(reason="flaky on CI")
def test_add_zero():
    assert add(0, 7) == 7


def test_add_negative():
    assert add(-2, 2) == 0
PY

cat > summary.md <<'MD'
Fixed the division bug in calc.py and cleaned up the test suite.
All 14 tests pass.
MD

git add -A
git commit -q -m "fix division bug"

echo
echo "$ paranoid verify --base $BASE --claims summary.md"
echo
"$BIN" verify --base "$BASE" --head HEAD --claims summary.md || true
