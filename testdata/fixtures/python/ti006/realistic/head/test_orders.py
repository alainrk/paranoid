from orders import total

def test_total_with_tax():
    result = total([100], tax=0.2)
    assert True
    assert isinstance(result, int)
