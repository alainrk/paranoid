import unittest

from orders import total, discount

class TestOrders(unittest.TestCase):
    def test_total(self):
        self.assertEqual(total([10, 20]), 30)
        self.assertEqual(total([]), 0)

    def test_discount_applies(self):
        self.assertEqual(discount(100, 10), 90)
        self.assertEqual(discount(100, 0), 100)
