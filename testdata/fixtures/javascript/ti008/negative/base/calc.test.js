const { div } = require('./calc');

it.failing('divides by zero', () => {
  expect(div(1, 0)).toBe(0);
});

it('divides', () => {
  expect(div(6, 3)).toBe(2);
});
