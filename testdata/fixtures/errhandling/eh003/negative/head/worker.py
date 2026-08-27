def process(job):
    try:
        run(job)
    except Exception:
        logging.exception("job failed: %s", job.id)
        raise
