import { exec } from "child_process";
import express from "express";

const app = express();

// VULNERABLE: user input passed directly to exec
app.get("/ping", (req, res) => {
  const host = req.query.host;
  exec("ping -c 1 " + host, (error, stdout) => {
    res.send(stdout);
  });
});
