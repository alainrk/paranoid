const { charge } = require('./payments');

it('charges the card', async () => {
  const result = await charge(1000);
  expect(result.status).toBe('approved');
});
