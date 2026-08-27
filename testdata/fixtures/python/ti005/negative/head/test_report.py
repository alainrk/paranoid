import unittest

from report import render

class TestReport(unittest.TestCase):
    def test_title(self):
        self.assertEqual(render("doc").title, "Full Quarterly Report")
