import requests


def fetch_user(user_id):
    resp = requests.get(f"https://api.example.com/users/{user_id}")
    resp.raise_for_status()
    return resp.json()


def notify(event):
    try:
        requests.post("https://api.example.com/events", json=event)
    except requests.RequestException:
        pass
