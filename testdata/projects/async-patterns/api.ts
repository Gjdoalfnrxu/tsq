export async function fetchUser(id: number): Promise<any> {
    const response = await fetch(`/api/users/${id}`);
    return await response.json();
}

export async function fetchPosts(userId: number): Promise<any[]> {
    const response = await fetch(`/api/users/${userId}/posts`);
    return await response.json();
}

export function syncHelper(x: number): number {
    return x * 2;
}
