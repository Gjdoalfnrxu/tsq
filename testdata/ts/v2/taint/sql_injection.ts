// Test fixture: SQL injection — tainted HTTP input flows to SQL query.
import { Request, Response } from "express";
import { query } from "./db";

function handleRequest(req: Request, res: Response) {
    const userInput = req.query.name; // TaintSource: http_input
    const sql = "SELECT * FROM users WHERE name = '" + userInput + "'";
    query(sql); // TaintSink: sql
    res.send("done");
}
