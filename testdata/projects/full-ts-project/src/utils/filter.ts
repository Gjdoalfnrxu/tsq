// Exercises: cross-module import (calls into format.ts), generic predicate type,
// re-export pattern.

import type { Predicate, User, UserRole } from "../types/user";
import { pluck } from "./format";

export function filterBy<T>(items: T[], predicate: Predicate<T>): T[] {
    return items.filter(predicate);
}

export function usersWithRole(users: User[], role: UserRole): User[] {
    return filterBy(users, (u) => u.role === role);
}

export function userIds(users: User[]): string[] {
    return pluck(users, "id");
}
