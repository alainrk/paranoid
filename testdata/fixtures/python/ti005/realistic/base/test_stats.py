import unittest

from stats import mean, variance

class TestStats(unittest.TestCase):
    def test_mean(self):
        self.assertAlmostEqual(mean([1.0, 2.0, 4.0]), 2.3333333, places=7)

    def test_variance(self):
        self.assertEqual(variance([2.0, 2.0]), 0.0)
