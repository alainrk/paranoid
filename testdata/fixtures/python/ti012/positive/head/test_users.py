import api

from users import fetch_user

def test_fetch(monkeypatch):
    monkeypatch.setattr(api, "get", lambda url: {"name": "ana"})
    assert fetch_user(1).name == "ana"
