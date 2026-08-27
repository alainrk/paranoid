const { add, sub } = require('./calc');

test('adds', () => {
  expect(add(1, 2)).toBe(3);
  expect(add(2, 2)).toBe(4);
  expect(add(0, 5)).toBe(5);
  expect(add(1, 1)).toBe(2);
  expect(add(3, 3)).toBe(6);
});

test('subtracts', () => {
  expect(sub(3, 2)).toBe(1);
});
