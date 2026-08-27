import time

from calc import div


def test_div():
    time.sleep(1)
    assert div(6, 3) == 2
    assert div(9, 3) == 3
