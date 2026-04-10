// simple_function.ts — basic function declarations and calls

function greet(name: string): string {
  return "Hello, " + name;
}

function add(a: number, b: number): number {
  return a + b;
}

const result = greet("world");
const sum = add(1, 2);

function outer() {
  function inner() {
    return 42;
  }
  return inner();
}
