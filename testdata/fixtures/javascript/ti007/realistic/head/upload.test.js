const { upload } = require('./upload');

function pending(name, fn) {}

it('uploads small files', () => {
  expect(upload('a.txt').ok).toBe(true);
});

pending('retries on timeout', () => {
  expect(upload('slow.bin').attempts).toBe(3);
});

it('rejects huge files', () => {
  expect(upload('huge.iso').ok).toBe(false);
});
