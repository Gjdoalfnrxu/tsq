// Features: conditional types (T extends U ? X : Y), infer keyword, distributive conditional

type IsString<T> = T extends string ? true : false;

type ElementType<T> = T extends (infer U)[] ? U : never;

type NonNullable2<T> = T extends null | undefined ? never : T;

type FunctionReturn<T> = T extends (...args: any[]) => infer R ? R : never;

type A = IsString<"hello">;
type B = IsString<42>;
type C = ElementType<number[]>;
type D = NonNullable2<string | null>;
type E = FunctionReturn<() => boolean>;

const check: A = true;
const elem: C = 42;
