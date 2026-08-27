from calc import add

def test_add():
    assert add(1, 2) == 3
    assert add(2, 2) == 4
    assert add(0, 5) == 5
    assert add(1, 1) == 2
    assert add(3, 3) == 6
    assert add(4, 4) == 8
    assert add(5, 5) == 10
    assert add(6, 6) == 12
    assert add(7, 7) == 14
    assert add(8, 8) == 16
