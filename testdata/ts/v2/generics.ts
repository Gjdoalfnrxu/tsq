// Test fixture: generics and complex inheritance

interface Repository<T> {
  find(id: string): T | undefined;
  save(entity: T): void;
  delete(id: string): boolean;
}

interface Identifiable {
  id: string;
}

class InMemoryRepository<T extends Identifiable> implements Repository<T> {
  private items: Map<string, T> = new Map();

  find(id: string): T | undefined {
    return this.items.get(id);
  }

  save(entity: T): void {
    this.items.set(entity.id, entity);
  }

  delete(id: string): boolean {
    return this.items.delete(id);
  }
}

class User implements Identifiable {
  constructor(public id: string, public name: string) {}
}

const repo = new InMemoryRepository<User>();
repo.save(new User("1", "Alice"));
const user = repo.find("1");
