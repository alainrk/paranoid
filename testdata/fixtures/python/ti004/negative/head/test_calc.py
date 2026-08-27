import sys

import pytest

from calc import add, sub

@pytest.mark.skipif(sys.platform == "win32", reason="posix only")
def test_add_posix():
    assert add(1, 2) == 3

@pytest.mark.skipif(not sys.stdin.isatty(), reason="needs a tty")
def test_sub_interactive():
    assert sub(3, 2) == 1
