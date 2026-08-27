const { charge } = require('./payments');

it.retry(3)('charges the card', async () => {
  const result = await charge(1000);
  expect(result.status).toBe('approved');
});
