const { div } = require('./calc');

it('divides', function () {
  this.timeout(2500);
  expect(div(6, 3)).toBe(2);
});
