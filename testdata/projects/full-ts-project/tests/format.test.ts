// Vitest-style test file. Exercises: import from src under test path,
// cross-module function call.

import { describe, it, expect } from "vitest";
import { greet, formatUserLabel, pluck } from "../src/utils/format";
import type { User } from "../src/types/user";

describe("format utils", () => {
    it("greets with default", () => {
        expect(greet("world")).toBe("Hello, world!");
    });

    it("greets with custom prefix", () => {
        expect(greet("world", "Hi")).toBe("Hi, world!");
    });

    it("formats a user label", () => {
        const u: User = { id: "u1", name: "Ada", email: "ada@example.com", role: "admin" };
        expect(formatUserLabel(u)).toBe("Ada <ada@example.com>");
    });

    it("plucks a field", () => {
        const users: User[] = [
            { id: "u1", name: "Ada", email: "a@x", role: "admin" },
            { id: "u2", name: "Bob", email: "b@x", role: "viewer" },
        ];
        expect(pluck(users, "name")).toEqual(["Ada", "Bob"]);
    });
});
