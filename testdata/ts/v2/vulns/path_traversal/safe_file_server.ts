import express from "express";
import fs from "fs";
import path from "path";

const app = express();

// SAFE: validates path before reading
app.get("/file", (req, res) => {
  const filename = req.query.path;
  const safePath = path.join("/uploads", path.basename(filename));
  fs.readFile(safePath, (err, data) => {
    res.send(data);
  });
});
