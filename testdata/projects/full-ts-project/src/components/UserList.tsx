// React-style component using a hook. Exercises: JSX, hook usage,
// cross-module call (formatUserLabel from utils/format), generic type
// parameter on useState, prop typing.

import { useState } from "react";
import type { User, UserRole } from "../types/user";
import { formatUserLabel, greet } from "../utils/format";
import { usersWithRole } from "../utils/filter";

interface UserListProps {
    users: User[];
    initialRole?: UserRole;
    title: string;
}

export function UserList({ users, initialRole = "viewer", title }: UserListProps) {
    const [role, setRole] = useState<UserRole>(initialRole);
    const visible = usersWithRole(users, role);
    const heading = greet(title);

    return (
        <section className="user-list">
            <h2>{heading}</h2>
            <select value={role} onChange={(e) => setRole(e.target.value as UserRole)}>
                <option value="admin">Admin</option>
                <option value="editor">Editor</option>
                <option value="viewer">Viewer</option>
            </select>
            <ul>
                {visible.map((user) => (
                    <li key={user.id}>{formatUserLabel(user)}</li>
                ))}
            </ul>
        </section>
    );
}

export default UserList;
