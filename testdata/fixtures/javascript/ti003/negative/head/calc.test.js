const { add } = require('./calc');

test('adds', () => {
  expect(add(1, 2)).toBe(3);
  expect(add(4, 4)).toBe(8);
});
