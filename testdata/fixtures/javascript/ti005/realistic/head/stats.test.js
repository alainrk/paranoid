const { mean } = require('./stats');

test('mean of samples', () => {
  expect(mean([1, 2, 4])).toBeCloseTo(2.3333333, 1);
});

test('mean of one', () => {
  expect(mean([7])).toBe(7);
});
