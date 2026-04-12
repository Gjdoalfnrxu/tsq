const express = require('express');
const app = express();
const db = require('./db');

app.get('/search', function handler(req, res) {
    const userInput = req.query.q;

    // XSS: user input flows to response body
    res.send('<html>' + userInput + '</html>');

    // SQL injection: user input flows to query string
    db.query('SELECT * FROM users WHERE name = ' + userInput);

    // eval: user input flows to eval
    eval(userInput);
});

class Sanitizer {
    clean(input: string): string { return input.replace(/</g, '&lt;'); }
}

function validate(data: string): boolean { return data.length > 0; }

function transform(v: string): string { return v.toUpperCase(); }

function encode(t: string): string { return encodeURIComponent(t); }
