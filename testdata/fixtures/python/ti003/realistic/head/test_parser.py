from parser import parse

def test_parse_title():
    doc = parse("# Title\nbody")
    assert doc.title == "Title"
    assert doc.body == "body"

def test_parse_empty():
    doc = parse("")
    assert doc.title == ""
    assert doc.body == ""
