import pytest

from calc import div


@pytest.mark.flaky(reruns=3)
def test_div():
    assert div(6, 3) == 2
