interface User {
  name: string;
  age: number;
}

type Status = "active" | "inactive";

function getUser(id: number): User {
  return { name: "Alice", age: 30 };
}

const user: User = getUser(1);
const status: Status = "active";
