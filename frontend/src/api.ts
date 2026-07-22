import type { CategoryTotal, Summary, Transaction, Upload, User } from "./types";

const BASE = "/api/v1";

/** Thrown when a request fails because there's no (or an expired) session. */
export class UnauthorizedError extends Error {
  constructor() {
    super("not authenticated");
    this.name = "UnauthorizedError";
  }
}

async function getJSON<T>(path: string): Promise<T> {
  const res = await fetch(BASE + path);
  if (res.status === 401) throw new UnauthorizedError();
  if (!res.ok) throw new Error(`${res.status} ${await res.text()}`);
  return res.json();
}

/** Logs in (or silently creates an account) with just an email — no password. */
export async function login(email: string): Promise<User> {
  const res = await fetch(`${BASE}/auth/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email }),
  });
  if (!res.ok) throw new Error((await res.json().catch(() => null))?.error ?? `${res.status}`);
  return res.json();
}

export async function logout(): Promise<void> {
  const res = await fetch(`${BASE}/auth/logout`, { method: "POST" });
  if (!res.ok) throw new Error(`${res.status} ${await res.text()}`);
}

/** Returns the current session's user, or throws UnauthorizedError if logged out. */
export const fetchMe = () => getJSON<User>("/auth/me");

export const fetchSummary = () => getJSON<Summary>("/analytics/summary");
export const fetchCategoryBreakdown = (month: string) =>
  getJSON<CategoryTotal[]>(
    `/analytics/categories${month ? `?month=${month}` : ""}`,
  );
export const fetchTransactions = (month: string, category: string) => {
  const params = new URLSearchParams();
  if (month) params.set("month", month);
  if (category) params.set("category", category);
  const qs = params.toString();
  return getJSON<Transaction[]>(`/transactions${qs ? `?${qs}` : ""}`);
};
export const fetchUploads = () => getJSON<Upload[]>("/uploads");
export const fetchCategories = () => getJSON<string[]>("/categories");
export const fetchVersion = () => getJSON<{ commit: string }>("/version");

/** Creates a custom category; rejects (409) names that already exist. */
export async function addCategory(name: string): Promise<void> {
  const res = await fetch(`${BASE}/categories`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name }),
  });
  if (res.status === 409) throw new Error("that category already exists");
  if (!res.ok) throw new Error(`${res.status} ${await res.text()}`);
}

/** Deletes all uploads and their transactions; category rules survive. */
export async function clearTransactions(): Promise<void> {
  const res = await fetch(`${BASE}/transactions`, { method: "DELETE" });
  if (!res.ok) throw new Error(`${res.status} ${await res.text()}`);
}

export async function recategorize(
  id: string,
  category: string,
): Promise<void> {
  const res = await fetch(`${BASE}/transactions/${id}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    // The backend persists every manual re-categorization as a user rule and
    // retags all same-merchant transactions.
    body: JSON.stringify({ category }),
  });
  if (!res.ok) throw new Error(`${res.status} ${await res.text()}`);
}

/** Upload with progress via XHR (fetch has no upload progress events). */
export function uploadFile(
  file: File,
  onProgress: (pct: number) => void,
): Promise<Upload> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("POST", `${BASE}/uploads`);
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) onProgress(Math.round((e.loaded / e.total) * 100));
    };
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve(JSON.parse(xhr.responseText));
      } else {
        let msg = `upload failed (${xhr.status})`;
        try {
          msg = JSON.parse(xhr.responseText).error ?? msg;
        } catch {
          /* keep default */
        }
        reject(new Error(msg));
      }
    };
    xhr.onerror = () => reject(new Error("network error during upload"));
    const form = new FormData();
    form.append("file", file);
    xhr.send(form);
  });
}

export const fmtMoney = new Intl.NumberFormat(undefined, {
  style: "currency",
  currency: import.meta.env.VITE_CURRENCY ?? "USD",
  maximumFractionDigits: 2,
}).format;
