const { div } = require('./calc');

it('divides', async () => {
  await new Promise((r) => setTimeout(r, 500));
  expect(div(6, 3)).toBe(2);
  expect(div(9, 3)).toBe(3);
});
