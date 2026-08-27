const { total } = require('./cart');

describe('total', () => {
  it('sums prices', () => {
    expect(total([{ price: 5 }, { price: 7 }])).toBe(12);
  });
});
