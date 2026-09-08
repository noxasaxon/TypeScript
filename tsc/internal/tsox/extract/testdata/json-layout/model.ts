export interface Principal { id: string; tenant: string; role: "reader" | "editor" }
export interface Task { id: number; tenant: string; title: string; done: boolean; tags: string[]; detail?: { owner: string; note?: string } }
export type Result<T> = { ok: true; value: T } | { ok: false; status: number; code: string };
export interface NewTask { title: string; tags: string[]; detail?: { owner: string; note?: string } }
