// TSOX standard-library contract for Node 24.20.0.
// This is the supported source API, not a replacement application declaration.
// Unsupported Node overloads remain explicit compiler boundaries.
declare module "node:fs/promises" {
    export function readFile(path: string, encoding: "utf8"): Promise<string>;
}
