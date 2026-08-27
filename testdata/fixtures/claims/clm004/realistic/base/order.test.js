const { total } = require('./order');

it('totals an order', () => {
  expect(total([{ price: 4 }])).toBe(4);
});
