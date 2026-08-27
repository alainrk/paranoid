const { div } = require('./calc');

jest.retryTimes(3);

it('divides', () => {
  expect(div(6, 3)).toBe(2);
  expect(div(9, 3)).toBe(3);
});
