from users import fetch_user

def test_fetch():
    assert fetch_user(1).name == "ana"
