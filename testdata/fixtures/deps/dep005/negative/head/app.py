import flask
import requests

app = flask.Flask(__name__)

@app.get("/proxy")
def proxy():
    return requests.get("https://example.com").text
