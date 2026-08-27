__all__ = ["compute_total", "helper"]


def compute_total(items):
    return sum(item.price for item in items)


def helper():
    return 1
