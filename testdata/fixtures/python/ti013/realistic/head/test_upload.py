from storage import upload


@flaky(max_runs=3, min_passes=1)
def test_upload_completes():
    result = upload("report.csv")
    assert result.status == "done"
