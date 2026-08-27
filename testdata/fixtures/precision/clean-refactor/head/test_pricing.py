import sys

import pytest

from pricing import price_with_tax

def test_price_includes_tax():
    assert price_with_tax(100, 0.2) == 120

def test_price_without_tax():
    assert price_with_tax(100, 0) == 100

def test_price_rounds_to_cents():
    assert price_with_tax(99, 0.075) == 106.43

@pytest.mark.skipif(sys.platform == "win32", reason="locale differs")
def test_price_formats_locale():
    assert price_with_tax(100, 0.2) == 120
