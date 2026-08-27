import flask
import requets

app = flask.Flask(__name__)

@app.get("/proxy")
def proxy():
    return requets.get("https://example.com").text
