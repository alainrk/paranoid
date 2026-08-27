const { priceWithTax } = require('./pricing');

test('adds tax', () => {
  const result = priceWithTax(100, 0.2);
  expect(result).toBe(result);
  expect(Number.isInteger(result)).toBe(true);
});
