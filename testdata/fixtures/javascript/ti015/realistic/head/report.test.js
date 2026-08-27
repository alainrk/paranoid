const { generate } = require('./report');

it('builds the quarterly report', function () {
  this.timeout(60000);
  expect(generate('q4').pages).toBeTruthy();
});
