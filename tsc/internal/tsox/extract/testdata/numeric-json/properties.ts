function inspect(value: unknown): string {
  if (typeof value !== "object" || value === null || !("n" in value)) return "missing";
  const n = value.n;
  return `number:${Number.isFinite(n)}:${Number.isInteger(n)}`;
}
