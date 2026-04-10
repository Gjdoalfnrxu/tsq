import React from "react";

interface Props {
  userInput: string;
}

// VULNERABLE: dangerouslySetInnerHTML with user-controlled props
export function BadComponent({ userInput }: Props) {
  return <div dangerouslySetInnerHTML={{ __html: userInput }} />;
}
