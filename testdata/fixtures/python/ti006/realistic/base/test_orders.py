from orders import total

def test_total_with_tax():
    result = total([100], tax=0.2)
    assert result == 120
    assert isinstance(result, int)
