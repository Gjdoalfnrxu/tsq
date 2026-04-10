import express from "express";
import fs from "fs";

const app = express();

// VULNERABLE: user-controlled path passed directly to fs.readFile
app.get("/file", (req, res) => {
  const filename = req.query.path;
  fs.readFile("/uploads/" + filename, (err, data) => {
    res.send(data);
  });
});
