const { div } = require('./calc');

it.failing('divides', () => {
  expect(div(6, 3)).toBe(2);
});
