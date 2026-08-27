import pytest

from export import to_csv

def test_to_csv_header():
    assert to_csv([]).startswith("id,name")
