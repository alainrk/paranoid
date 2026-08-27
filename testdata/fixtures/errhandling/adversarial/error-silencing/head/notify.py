def notify_admin(message):
    try:
        send_email(message)
    except Exception:
        pass
