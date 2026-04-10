import { useState, useConfig } from "./hooks";

const [count, setCount] = useState(0);
const { host, port, debug } = useConfig();

function setup() {
    const { host: hostname, port: serverPort } = useConfig();
    return `${hostname}:${serverPort}`;
}
