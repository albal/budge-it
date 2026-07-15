import { useCallback, useEffect, useRef, useState } from "react";
import {
  fetchCategories,
  fetchCategoryBreakdown,
  fetchSummary,
  fetchTransactions,
  fetchUploads,
} from "./api";
import type { CategoryTotal, Summary, Transaction, Upload } from "./types";
import CategoryChart from "./components/CategoryChart";
import MonthlyFlowChart from "./components/MonthlyFlowChart";
import StatTile from "./components/StatTile";
import TransactionTable from "./components/TransactionTable";
import UploadDropzone from "./components/UploadDropzone";

export default function App() {
  const [summary, setSummary] = useState<Summary | null>(null);
  const [breakdown, setBreakdown] = useState<CategoryTotal[]>([]);
  const [txns, setTxns] = useState<Transaction[]>([]);
  const [uploads, setUploads] = useState<Upload[]>([]);
  const [categories, setCategories] = useState<string[]>([]);
  const [month, setMonth] = useState("");
  const [category, setCategory] = useState("");
  const [error, setError] = useState("");
  const pollRef = useRef<number | null>(null);

  const refreshData = useCallback(async () => {
    try {
      const [sum, cats] = await Promise.all([
        fetchSummary(),
        fetchCategoryBreakdown(month),
      ]);
      setSummary(sum);
      setBreakdown(cats);
      setTxns(await fetchTransactions(month, category));
      setError("");
    } catch (err) {
      setError((err as Error).message);
    }
  }, [month, category]);

  // Poll upload statuses while any statement is still being processed, and
  // refresh the dashboard as each one completes.
  const pollUploads = useCallback(async () => {
    try {
      const list = await fetchUploads();
      setUploads((prev) => {
        const newlyDone = list.some(
          (u) => u.status === "done" &&
            prev.find((p) => p.id === u.id)?.status !== "done",
        );
        if (newlyDone) void refreshData();
        return list;
      });
      const busy = list.some((u) => u.status === "pending" || u.status === "processing");
      if (busy && pollRef.current === null) {
        pollRef.current = window.setInterval(() => void pollUploads(), 2500);
      } else if (!busy && pollRef.current !== null) {
        window.clearInterval(pollRef.current);
        pollRef.current = null;
      }
    } catch {
      /* transient; next poll retries */
    }
  }, [refreshData]);

  useEffect(() => {
    fetchCategories().then(setCategories).catch(() => setCategories([]));
    void pollUploads();
    return () => {
      if (pollRef.current !== null) window.clearInterval(pollRef.current);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    void refreshData();
  }, [refreshData]);

  const busyCount = uploads.filter(
    (u) => u.status === "pending" || u.status === "processing",
  ).length;

  return (
    <div className="container">
      <header className="topbar">
        <h1>Budge-it</h1>
        <span className="tagline">know where your money goes</span>
      </header>

      {error && (
        <div className="card" style={{ color: "var(--delta-bad)" }}>
          Backend unavailable: {error}
        </div>
      )}

      <UploadDropzone onAccepted={() => void pollUploads()} />
      {busyCount > 0 && (
        <div className="card status-chip">
          Processing {busyCount} statement{busyCount > 1 ? "s" : ""}…
        </div>
      )}

      <div className="kpi-row">
        <StatTile label="Total money in" value={summary?.totalInflow ?? 0} />
        <StatTile label="Total money out" value={summary?.totalOutflow ?? 0} />
        <StatTile label="Net" value={summary?.net ?? 0} signed />
      </div>

      <div className="filter-row">
        <label htmlFor="f-month">Month</label>
        <select id="f-month" value={month} onChange={(e) => setMonth(e.target.value)}>
          <option value="">All months</option>
          {summary?.months.map((m) => (
            <option key={m.month} value={m.month}>{m.month}</option>
          ))}
        </select>
        <label htmlFor="f-cat">Category</label>
        <select id="f-cat" value={category} onChange={(e) => setCategory(e.target.value)}>
          <option value="">All categories</option>
          {categories.map((c) => (
            <option key={c} value={c}>{c}</option>
          ))}
        </select>
      </div>

      <div className="grid-2">
        <div className="card">
          <h2>Monthly in vs out</h2>
          <MonthlyFlowChart months={summary?.months ?? []} />
        </div>
        <div className="card">
          <h2>Spending by category{month ? ` — ${month}` : ""}</h2>
          <CategoryChart data={breakdown} />
        </div>
      </div>

      <div className="card">
        <h2>Transactions</h2>
        <TransactionTable txns={txns} categories={categories} onChanged={() => void refreshData()} />
      </div>

      {uploads.length > 0 && (
        <div className="card">
          <h2>Upload history</h2>
          {uploads.map((u) => (
            <div className="upload-item" key={u.id}>
              <span className="name">{u.filename}</span>
              {u.status === "done" && (
                <span className="status-chip done">✓ {u.txnCount} transactions</span>
              )}
              {u.status === "error" && (
                <span className="status-chip error">✕ {u.error}</span>
              )}
              {(u.status === "pending" || u.status === "processing") && (
                <span className="status-chip">⏳ {u.status}</span>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
