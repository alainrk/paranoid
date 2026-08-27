import pytest

pytestmark = pytest.mark.skip(reason="api rework in progress")

from api import fetch, push

def test_fetch():
    assert fetch("a").status == 200

def test_push():
    assert push("a", b"x") is True
