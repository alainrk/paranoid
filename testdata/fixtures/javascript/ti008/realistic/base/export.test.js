const { toCsv } = require('./export');

test('writes the header', () => {
  expect(toCsv([])).toMatch(/^id,name/);
});
