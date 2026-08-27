const { displayName } = require('./user');

test('formats the display name', () => {
  expect(displayName({ first: 'Ana', last: 'Gil' })).toBe('Ana Gil');
});
