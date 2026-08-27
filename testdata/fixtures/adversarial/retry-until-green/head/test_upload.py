import time

from storage import upload


def test_upload_completes():
    time.sleep(3)
    result = upload("report.csv")
    assert result.status == "done"
