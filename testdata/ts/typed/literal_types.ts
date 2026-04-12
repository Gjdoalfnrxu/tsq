// Features: string literal types, numeric literal types, template literal types, const assertions

type Direction = "north" | "south" | "east" | "west";

type HttpStatus = 200 | 301 | 404 | 500;

type EventName = `on${Capitalize<Direction>}`;

function move(dir: Direction): void {}

function handleStatus(code: HttpStatus): string {
  switch (code) {
    case 200: return "ok";
    case 301: return "redirect";
    case 404: return "not found";
    case 500: return "error";
  }
}

const config = { endpoint: "/api", retries: 3 } as const;

move("north");
const status = handleStatus(200);
