// Features: generic function, generic class, bounded generic (T extends Interface)

interface HasLength {
  length: number;
}

function identity<T>(value: T): T {
  return value;
}

function longest<T extends HasLength>(a: T, b: T): T {
  return a.length >= b.length ? a : b;
}

class Box<T> {
  constructor(public value: T) {}

  map<U>(fn: (val: T) => U): Box<U> {
    return new Box(fn(this.value));
  }
}

const num = identity(42);
const str = longest("hello", "hi");
const box = new Box(10).map((n) => n.toString());
