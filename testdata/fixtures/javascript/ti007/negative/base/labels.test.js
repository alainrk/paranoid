const { label } = require('./labels');
const cases = ['adds', 'subs'];

it('adds', () => {
  expect(label('adds')).toBe('Addition');
});

it('labels every case', () => {
  expect(cases.map(label)).toHaveLength(2);
  expect(label('subs')).toBe('Subtraction');
});
