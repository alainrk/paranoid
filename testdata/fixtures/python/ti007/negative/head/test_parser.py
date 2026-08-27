from parser import parse

def build_input(kind):
    return kind * 2

def test_parse():
    assert parse("x") == 1
    assert parse(build_input("y")) == 2
