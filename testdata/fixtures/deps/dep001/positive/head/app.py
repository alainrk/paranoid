import flask
import requests_toolkit_pro

app = flask.Flask(__name__)

@app.get("/fetch")
def fetch():
    return requests_toolkit_pro.get("https://example.com").text
