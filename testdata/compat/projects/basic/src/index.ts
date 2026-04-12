const express = require('express');
const app = express();
const db = require('./db');

app.get('/search', function(req, res) {
    const userInput = req.query.q;

    // XSS: user input flows to response body
    res.send('<html>' + userInput + '</html>');

    // SQL injection: user input flows to query string
    db.query('SELECT * FROM users WHERE name = ' + userInput);

    // eval: user input flows to eval
    eval(userInput);
});
