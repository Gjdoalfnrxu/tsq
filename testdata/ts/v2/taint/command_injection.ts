// Test fixture: command injection — tainted input flows to exec.
import { Request, Response } from "express";
import { exec } from "child_process";

function runCommand(req: Request, res: Response) {
    const cmd = req.body.command; // TaintSource: http_input
    exec(cmd); // TaintSink: command_injection
    res.send("executed");
}
