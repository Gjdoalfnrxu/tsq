import { execFile } from "child_process";
import express from "express";

const app = express();

// SAFE: execFile with array args prevents command injection
app.get("/ping", (req, res) => {
  const host = req.query.host;
  execFile("ping", ["-c", "1", host], (error, stdout) => {
    res.send(stdout);
  });
});
