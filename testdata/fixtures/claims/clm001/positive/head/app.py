def handle(request):
    if request is None:
        return {"status": "bad request"}
    return {"status": "ok"}
