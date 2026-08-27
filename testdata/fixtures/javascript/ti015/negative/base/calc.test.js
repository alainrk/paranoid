const { div } = require('./calc');

it('divides', function () {
  this.timeout(1000);
  expect(div(6, 3)).toBe(2);
});
