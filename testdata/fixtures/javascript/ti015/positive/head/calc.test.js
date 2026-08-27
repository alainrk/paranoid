const { div } = require('./calc');

it('divides', function () {
  this.timeout(10000);
  expect(div(6, 3)).toBe(2);
});
