const { validate } = require('./validate');

describe('validate', () => {
  it('accepts a valid payload', () => {
    expect(validate({ name: 'ana' }).ok).toBe(true);
    expect(validate({ name: 'ana' }).errors).toHaveLength(0);
  });

  it('rejects a missing name', () => {
    const result = validate({});
    expect(result.ok).toBe(false);
    expect(result.errors).toContain('name is required');
    expect(result.errors).toHaveLength(1);
  });
});
