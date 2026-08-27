from pricing import price_with_tax

def test_price_with_tax():
    assert price_with_tax(100, 0.2) == 120

def test_price_without_tax():
    assert price_with_tax(100, 0) == 100
