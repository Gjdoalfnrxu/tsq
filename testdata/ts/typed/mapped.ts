// Features: mapped types ({ [K in keyof T]: ... }), key remapping, readonly/optional modifiers

interface User {
  name: string;
  age: number;
  email: string;
}

type ReadonlyAll<T> = { readonly [K in keyof T]: T[K] };

type Optional<T> = { [K in keyof T]?: T[K] };

type Getters<T> = {
  [K in keyof T as `get${Capitalize<string & K>}`]: () => T[K];
};

type Nullable<T> = { [K in keyof T]: T[K] | null };

const frozen: ReadonlyAll<User> = { name: "Ada", age: 36, email: "a@b.c" };
const partial: Optional<User> = { name: "Grace" };
const nullable: Nullable<User> = { name: null, age: 85, email: "g@h.c" };
