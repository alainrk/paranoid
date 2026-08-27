const { charge } = require('./payments');

it('charges the card', function () {
  this.timeout(3000);
  expect(charge(1000).status).toBe('approved');
});
