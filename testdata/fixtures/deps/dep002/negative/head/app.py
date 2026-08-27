import flask
import requests

app = flask.Flask(__name__)

@app.get("/status")
def status():
    return requests.get("https://example.com/health").text
