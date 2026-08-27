def fetch(url):
    return retry_with_backoff(lambda: http.get(url))

def retry_with_backoff(op, attempts=3):
    for i in range(attempts):
        try:
            return op()
        except IOError:
            continue
    raise IOError("gave up")
