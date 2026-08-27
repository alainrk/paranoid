const { add, sub } = require('./calc');

xit('adds huge numbers', () => {
  expect(add(1e308, 1e308)).toBe(Infinity);
});

test('adds', () => {
  expect(add(1, 2)).toBe(3);
});
