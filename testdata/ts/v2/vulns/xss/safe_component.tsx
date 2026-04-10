import React from "react";

function escapeHtml(s: string): string {
  return s.replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

interface Props {
  userInput: string;
}

// SAFE: escapeHtml sanitizer applied before rendering
export function SafeComponent({ userInput }: Props) {
  const safe = escapeHtml(userInput);
  return <div>{safe}</div>;
}
