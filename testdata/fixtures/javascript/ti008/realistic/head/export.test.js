const { toCsv } = require('./export');

test('writes the header', () => {
  expect(toCsv([])).toMatch(/^id,name/);
});

test.failing('escapes commas in names', () => {
  expect(toCsv([{ id: 1, name: 'Gil, Ana' }])).toContain('"Gil, Ana"');
});
