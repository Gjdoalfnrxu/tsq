// Consumer — imports only from mod_alpha. Under PR6/PR7 semantics the
// closure follows the name, not the module specifier; the resolution
// set for `action` is expected to include BOTH alpha and beta sources.

import { action } from './mod_alpha';

export function go(): string {
  return action();
}
