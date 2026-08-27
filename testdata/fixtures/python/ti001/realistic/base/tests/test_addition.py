from app.calc import add

def test_add_small():
    assert add(1, 2) == 3

def test_add_zero():
    assert add(0, 7) == 7
