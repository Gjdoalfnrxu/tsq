import React from "react";
import { Button } from "./Button";

interface AppProps {
    title: string;
    env: string;
}

function App(props: AppProps) {
    return (
        <div className="app">
            <h1>{props.title}</h1>
            <Button label="Click me" onClick={() => console.log("clicked")} />
            <Button label="Submit" disabled={true} env={props.env} />
        </div>
    );
}

export default App;
