import sys

import pytest

from calc import add

@pytest.mark.skipif(sys.platform == "win32", reason="posix only")
def test_add_posix():
    assert add(1, 2) == 3
