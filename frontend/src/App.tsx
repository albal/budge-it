import { useCallback, useEffect, useRef, useState } from "react";
import {
  addCategory,
  clearTransactions,
  fetchCategories,
  fetchCategoryBreakdown,
  fetchMe,
  fetchSummary,
  fetchTransactions,
  fetchUploads,
  fetchVersion,
  logout,
  UnauthorizedError,
} from "./api";
import type { CategoryTotal, Summary, Transaction, Upload, User } from "./types";
import CategoryChart from "./components/CategoryChart";
import LoginForm from "./components/LoginForm";
import MonthlyFlowChart from "./components/MonthlyFlowChart";
import StatTile from "./components/StatTile";
import TransactionTable from "./components/TransactionTable";
import UploadDropzone from "./components/UploadDropzone";

export default function App() {
  // undefined = session check in flight, null = logged out
  const [user, setUser] = useState<User | null | undefined>(undefined);
  const [summary, setSummary] = useState<Summary | null>(null);
  const [breakdown, setBreakdown] = useState<CategoryTotal[]>([]);
  const [txns, setTxns] = useState<Transaction[]>([]);
  const [uploads, setUploads] = useState<Upload[]>([]);
  const [categories, setCategories] = useState<string[]>([]);
  const [month, setMonth] = useState("");
  const [category, setCategory] = useState("");
  const [error, setError] = useState("");
  const [commit, setCommit] = useState("");
  // Kibana observability links (empty when the Elastic stack isn't deployed)
  const [kibanaUrl, setKibanaUrl] = useState("");
  const [dashboardUrl, setDashboardUrl] = useState("");
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
    fetchVersion()
      .then((v) => {
        setCommit(v.commit);
        setKibanaUrl(v.kibanaUrl ?? "");
        setDashboardUrl(v.kibanaDashboardUrl ?? "");
      })
      .catch(() => setCommit(""));
    fetchMe()
      .then(setUser)
      .catch((err) => {
        setUser(null);
        if (!(err instanceof UnauthorizedError)) setError((err as Error).message);
      });
  }, []);

  useEffect(() => {
    if (!user) return;
    fetchCategories().then(setCategories).catch(() => setCategories([]));
    void pollUploads();
    return () => {
      if (pollRef.current !== null) window.clearInterval(pollRef.current);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [user]);

  useEffect(() => {
    if (!user) return;
    void refreshData();
  }, [user, refreshData]);

  const handleLogout = async () => {
    try {
      await logout();
    } finally {
      setUser(null);
      setSummary(null);
      setBreakdown([]);
      setTxns([]);
      setUploads([]);
    }
  };

  const busyCount = uploads.filter(
    (u) => u.status === "pending" || u.status === "processing",
  ).length;

  // With a month selected the KPI tiles summarise that month; otherwise all time.
  const monthFlow = month
    ? summary?.months.find((m) => m.month === month)
    : undefined;
  const inflow = monthFlow ? monthFlow.inflow : summary?.totalInflow ?? 0;
  const outflow = monthFlow ? monthFlow.outflow : summary?.totalOutflow ?? 0;
  const kpiSuffix = month ? ` — ${month}` : "";

  if (user === undefined) return null;
  if (user === null) return <LoginForm onLoggedIn={setUser} />;

  return (
    <div className="container">
      <header className="topbar">
        <h1>Budge-it</h1>
        <span className="tagline">know where your money goes</span>
        {(dashboardUrl || kibanaUrl) && (
          <nav className="obs-links">
            {dashboardUrl && (
              <a href={dashboardUrl} target="_blank" rel="noreferrer">
                📊 Dashboard
              </a>
            )}
            {kibanaUrl && (
              <a href={kibanaUrl} target="_blank" rel="noreferrer">
                Kibana
              </a>
            )}
          </nav>
        )}
        <span className="session-info">
          {user.email}
          <button className="link-btn" onClick={() => void handleLogout()}>Log out</button>
        </span>
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
        <StatTile label={`Money in${kpiSuffix}`} value={inflow} />
        <StatTile label={`Money out${kpiSuffix}`} value={outflow} />
        <StatTile label={`Net${kpiSuffix}`} value={inflow - outflow} signed />
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
        <div className="card-header-row">
          <h2>Transactions</h2>
          {txns.length > 0 && (
            <button
              className="danger-btn"
              onClick={() => {
                if (!window.confirm(
                  "Delete ALL transactions and upload history? Your category rules are kept.",
                )) return;
                clearTransactions()
                  .then(() => void refreshData())
                  .then(() => void pollUploads())
                  .catch((err) => setError((err as Error).message));
              }}
            >
              Clear transactions
            </button>
          )}
        </div>
        <TransactionTable
          txns={txns}
          categories={categories}
          onChanged={() => void refreshData()}
          onAddCategory={async (name) => {
            await addCategory(name);
            setCategories(await fetchCategories());
          }}
        />
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

      <footer className="footer">
        Budge-it{commit ? ` · build ${commit.slice(0, 7)}` : ""}
      </footer>
    </div>
  );
}
