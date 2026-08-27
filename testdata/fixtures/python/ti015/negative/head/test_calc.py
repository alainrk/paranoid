import pytest

from calc import div


@pytest.mark.timeout(10)
def test_div():
    assert div(6, 3) == 2
