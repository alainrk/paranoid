const { add } = require('./calc');

checkLater('adds', () => {
  expect(add(1, 2)).toBe(3);
});
