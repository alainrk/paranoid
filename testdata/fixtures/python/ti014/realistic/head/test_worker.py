import asyncio

from worker import enqueue


async def test_job_finishes():
    job = await enqueue("report")
    await asyncio.sleep(2)
    assert job == "report"
