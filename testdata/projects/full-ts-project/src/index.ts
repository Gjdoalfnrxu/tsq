// Barrel re-exports — entry point.

export { UserList } from "./components/UserList";
export { formatUserLabel, greet, ok, err, pluck } from "./utils/format";
export { filterBy, usersWithRole, userIds } from "./utils/filter";
export type { User, UserId, UserRole, Result, Predicate } from "./types/user";
