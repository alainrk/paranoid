import pytest

from export import to_csv

def test_to_csv_header():
    assert to_csv([]).startswith("id,name")

@pytest.mark.xfail(reason="unicode names not handled yet")
def test_to_csv_unicode():
    assert "José" in to_csv([{"id": 1, "name": "José"}])
