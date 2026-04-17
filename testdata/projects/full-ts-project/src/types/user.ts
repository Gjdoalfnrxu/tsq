// Type-only module — no runtime exports.
// Exercises: type alias, generic, union, exported types referenced cross-module.

export type UserId = string;

export type UserRole = "admin" | "editor" | "viewer";

export interface User {
    id: UserId;
    name: string;
    email: string;
    role: UserRole;
}

export type Result<T, E = Error> =
    | { ok: true; value: T }
    | { ok: false; error: E };

export type Predicate<T> = (value: T) => boolean;
