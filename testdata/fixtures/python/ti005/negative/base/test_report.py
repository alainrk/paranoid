import unittest

from report import render

class TestReport(unittest.TestCase):
    def test_title(self):
        self.assertIn("Report", render("doc").title)
