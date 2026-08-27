const { priceWithTax } = require('./pricing');

test('adds tax', () => {
  const result = priceWithTax(100, 0.2);
  expect(result).toBe(120);
  expect(Number.isInteger(result)).toBe(true);
});
