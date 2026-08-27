const express = require('express');
const hot = require('fresh-hotness');

const app = express();
app.use(hot());
