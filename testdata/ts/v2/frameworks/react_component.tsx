import React from "react";

interface Props {
  userContent: string;
}

// XSS via dangerouslySetInnerHTML
function UnsafeComponent({ userContent }: Props) {
  return (
    <div dangerouslySetInnerHTML={{ __html: userContent }} />
  );
}

// Safe component — no dangerouslySetInnerHTML
function SafeComponent({ userContent }: Props) {
  return <div>{userContent}</div>;
}

export { UnsafeComponent, SafeComponent };
