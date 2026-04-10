import { fetchUser, fetchPosts } from "./api";

async function loadUserData(id: number) {
    const user = await fetchUser(id);
    const posts = await fetchPosts(user.id);
    return { user, posts };
}

async function processAll(ids: number[]) {
    const results = await Promise.all(ids.map(id => loadUserData(id)));
    return results;
}
