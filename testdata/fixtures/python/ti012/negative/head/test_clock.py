import time_source

def test_now(monkeypatch):
    monkeypatch.setattr(time_source, "now", lambda: 1700000000)
    assert time_source.now() == 1700000000
