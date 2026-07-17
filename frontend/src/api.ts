import type { CategoryTotal, Summary, Transaction, Upload } from "./types";

const BASE = "/api/v1";

async function getJSON<T>(path: string): Promise<T> {
  const res = await fetch(BASE + path);
  if (!res.ok) throw new Error(`${res.status} ${await res.text()}`);
  return res.json();
}

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
