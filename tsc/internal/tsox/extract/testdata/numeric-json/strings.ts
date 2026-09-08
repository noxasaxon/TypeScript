function inspect(value: unknown): string {
  if (typeof value !== "string") return "kind";
  const parsed = Number(value);
  return `${parsed}:${1 / parsed}:${Number.isFinite(parsed)}:${Number.isInteger(parsed)}`;
}
