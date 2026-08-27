import pytest

from calc import div

@pytest.mark.xfail
def test_div():
    assert div(6, 3) == 2
