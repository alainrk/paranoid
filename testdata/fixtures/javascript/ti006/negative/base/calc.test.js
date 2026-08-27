const { add } = require('./calc');

test('adds', () => {
  expect(true).toBe(true);
  expect(add(1, 2)).toBe(3);
});
