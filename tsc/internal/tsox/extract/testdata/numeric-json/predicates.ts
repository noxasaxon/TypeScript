function inspect(value: unknown): string {
  if (Number.isFinite(value)) {
    if (typeof value !== "number") return "incorrect-kind";
    return `finite:${Number(value)}:${Number.isInteger(value)}`;
  }
  if (typeof value === "number") return `nonfinite:${Number(value)}:${Number.isInteger(value)}`;
  return `other:${Number.isInteger(value)}`;
}
