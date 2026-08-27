const { add } = require('./calc');

test('adds', () => {
  expect(add(1, 2)).toBe(3);
  expect(true).toBe(true);
});
