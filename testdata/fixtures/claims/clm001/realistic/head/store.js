function save(record) {
  if (!record.id) {
    throw new Error('record needs an id');
  }
  return db.insert(record);
}
module.exports = { save };
