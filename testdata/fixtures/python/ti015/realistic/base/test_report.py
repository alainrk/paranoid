import pytest

from report import generate


@pytest.mark.timeout(8)
def test_generate_finishes_quickly():
    assert generate("q4").pages > 0
