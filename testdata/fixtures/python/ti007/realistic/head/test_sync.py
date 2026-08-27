from sync import pull, push

def test_pull():
    assert pull("repo").ok

def _test_push_retries():
    assert push("repo", retries=3).attempts <= 3

def test_push_gives_up():
    assert push("repo", retries=0).ok is False
