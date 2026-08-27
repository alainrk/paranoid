const { add } = require('./calc');

test('adds', () => {
  expect(add(1, 2)).toBe(3);
  expect(add(2, 2)).toBe(4);
  expect(add(0, 5)).toBe(5);
  expect(add(1, 1)).toBe(2);
  expect(add(3, 3)).toBe(6);
  expect(add(4, 4)).toBe(8);
  expect(add(5, 5)).toBe(10);
  expect(add(6, 6)).toBe(12);
  expect(add(7, 7)).toBe(14);
  expect(add(8, 8)).toBe(16);
});
