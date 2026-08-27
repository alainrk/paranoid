from storage import upload


def test_upload_completes():
    result = upload("report.csv")
    assert result.status == "done"
