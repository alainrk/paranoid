from unittest import mock

from billing import charge, refund

def test_charge():
    assert charge(100).ok

def test_refund():
    assert refund(100).ok
