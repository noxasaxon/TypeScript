function inspect(input: unknown): number {
  const value: number = JSON.parse("null");
  return Number(value);
}
