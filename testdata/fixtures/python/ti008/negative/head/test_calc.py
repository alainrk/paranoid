import pytest

from calc import div

@pytest.mark.xfail
def test_div_by_zero():
    assert div(1, 0) == 0

def test_div():
    assert div(6, 3) == 2
    assert div(9, 3) == 3
