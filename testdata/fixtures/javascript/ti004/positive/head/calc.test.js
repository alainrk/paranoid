const { add } = require('./calc');

test.skip('adds', () => {
  expect(add(1, 2)).toBe(3);
});
