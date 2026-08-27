const { mul } = require('./calc');

test('multiplication', () => {
  expect(mul(2, 3)).toBe(6);
  expect(mul(0, 9)).toBe(0);
});
