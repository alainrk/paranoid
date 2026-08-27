from app.calc import div

def test_div_exact():
    assert div(6, 3) == 2

def test_div_rounds_down():
    assert div(7, 2) == 3
