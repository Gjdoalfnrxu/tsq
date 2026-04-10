import React from "react";

interface ButtonProps {
    label: string;
    onClick?: () => void;
    disabled?: boolean;
    env?: string;
}

function Button({ label, onClick, disabled, env }: ButtonProps) {
    return (
        <button className="btn" onClick={onClick} disabled={disabled}>
            {label}
        </button>
    );
}

export { Button };
