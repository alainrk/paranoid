from report import render

def test_header():
    doc = render("q1")
    assert doc.title == "Q1"
    assert doc.author == "finance"
    assert doc.pages > 0
    assert doc.toc is not None

def test_body():
    doc = render("q1")
    assert "revenue" in doc.body
    assert "costs" in doc.body
    assert "outlook" in doc.body

def test_footer():
    doc = render("q1")
    assert doc.footer.year == 2026
    assert doc.footer.page_numbers
    assert doc.footer.contact == "finance@example.com"
