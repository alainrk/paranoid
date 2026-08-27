from unittest import mock

from billing import charge, refund

@mock.patch("billing.gateway.submit")
def test_charge(submit):
    submit.return_value = {"ok": True}
    assert charge(100).ok

def test_refund():
    assert refund(100).ok
