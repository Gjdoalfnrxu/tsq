// Exported util module — pure functions consumed by components.
// Exercises: exported function, generic, default param, type-only import.

import type { User, Result } from "../types/user";

export function formatUserLabel(user: User): string {
    return `${user.name} <${user.email}>`;
}

export function ok<T>(value: T): Result<T> {
    return { ok: true, value };
}

export function err<T, E = Error>(error: E): Result<T, E> {
    return { ok: false, error };
}

export function pluck<T, K extends keyof T>(items: T[], key: K): T[K][] {
    return items.map((item) => item[key]);
}

export const DEFAULT_GREETING = "Hello";

export function greet(name: string, greeting: string = DEFAULT_GREETING): string {
    return `${greeting}, ${name}!`;
}
