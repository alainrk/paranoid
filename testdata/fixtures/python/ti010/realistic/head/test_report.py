from report import render

def test_header():
    doc = render("q1")
    assert doc.title == "Q1"
    assert doc.author == "finance"

def test_body():
    doc = render("q1")
    assert "revenue" in doc.body
    assert "costs" in doc.body

def test_footer():
    doc = render("q1")
    assert doc.footer.year == 2026
    assert doc.footer.page_numbers
    assert doc.footer.contact == "finance@example.com"
