// Cross-file taint source: returns tainted data from process.env
export function getConfig(): string {
  return process.env.USER_INPUT;
}
