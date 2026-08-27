const { div } = require('./calc');

it('divides', () => {
  expect(div(6, 3)).toBe(2);
});
