const express = require('express');
const ghost = require('ghost-http-client');

const app = express();
app.get('/', (req, res) => res.send('ok'));
app.get('/proxy', async (req, res) => res.send(await ghost.fetch(req.query.url)));
