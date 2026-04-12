// Features: union types, intersection types, discriminated unions, type narrowing

interface Circle {
  kind: "circle";
  radius: number;
}

interface Rectangle {
  kind: "rectangle";
  width: number;
  height: number;
}

type Shape = Circle | Rectangle;

function area(shape: Shape): number {
  switch (shape.kind) {
    case "circle":
      return Math.PI * shape.radius ** 2;
    case "rectangle":
      return shape.width * shape.height;
  }
}

type Timestamped = { createdAt: Date };
type Named = { name: string };
type Entity = Timestamped & Named;

const entity: Entity = { name: "test", createdAt: new Date() };
const a = area({ kind: "circle", radius: 5 });
