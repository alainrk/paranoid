const { enqueue } = require('./worker');

it('finishes the job', (done) => {
  const job = enqueue('report');
  setTimeout(() => {
    expect(job).toBe('report');
    done();
  }, 2000);
});
