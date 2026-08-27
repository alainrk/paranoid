from calc import add

def test_add():
    assert add(1, 2) == 3
    assert add(2, 2) == 4
    assert add(0, 5) == 5
    assert add(1, 1) == 2
