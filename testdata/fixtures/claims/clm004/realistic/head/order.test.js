const { total } = require('./order');

it('totals an order', () => {
  expect(total([{ price: 4 }])).toBe(4);
});

it('totals an empty order', () => {
  expect(total([])).toBe(0);
});
