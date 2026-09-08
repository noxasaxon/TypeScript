import type { NewTask, Result } from "./model.ts";
export function validateTask(value: unknown): Result<NewTask> {
  const bad: Result<NewTask> = { ok: false, status: 422, code: "invalid-task" };
  if (typeof value !== "object" || value === null || !("title" in value) || typeof value.title !== "string" || value.title.trim().length === 0 || value.title.length > 120) return bad;
  const tags: string[] = [];
  if ("tags" in value) {
    if (!Array.isArray(value.tags) || value.tags.length > 8) return bad;
    for (const tag of value.tags) {
      if (typeof tag !== "string" || tag.length > 32) return bad;
      if (!tags.includes(tag)) tags.push(tag);
    }
  }
  const task: NewTask = { title: value.title.trim(), tags };
  if ("detail" in value) {
    const detail = value.detail;
    if (typeof detail !== "object" || detail === null || !("owner" in detail) || typeof detail.owner !== "string") return bad;
    task.detail = { owner: detail.owner };
    if ("note" in detail) {
      if (typeof detail.note !== "string") return bad;
      task.detail.note = detail.note;
    }
  }
  return { ok: true, value: task };
}
