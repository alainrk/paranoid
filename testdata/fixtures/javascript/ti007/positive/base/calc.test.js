const { add } = require('./calc');

it('adds', () => {
  expect(add(1, 2)).toBe(3);
});
