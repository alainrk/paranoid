import pytest

from api import fetch, push

def test_fetch():
    assert fetch("a").status == 200

def test_push():
    assert push("a", b"x") is True
