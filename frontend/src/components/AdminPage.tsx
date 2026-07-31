import { useCallback, useEffect, useState } from "react";
import { deleteAdminUser, fetchAdminUsers, ForbiddenError } from "../api";
import type { AdminUser, User } from "../types";

/**
 * Lists every account and allows deleting one along with all of its data.
 *
 * Reachable only when the signed-in user is on the server's ADMIN_EMAILS
 * allowlist; the backend enforces that independently of this component, so a
 * non-admin who navigates here directly gets an explanatory 403 rather than
 * an empty page.
 */
export default function AdminPage({
  currentUser,
  onBack,
}: {
  currentUser: User;
  onBack: () => void;
}) {
  const [users, setUsers] = useState<AdminUser[] | null>(null);
  const [error, setError] = useState("");
  const [forbidden, setForbidden] = useState(false);
  // id of the account currently being deleted, so only its button spins
  const [deleting, setDeleting] = useState("");

  const load = useCallback(async () => {
    try {
      setUsers(await fetchAdminUsers());
      setError("");
      setForbidden(false);
    } catch (err) {
      if (err instanceof ForbiddenError) {
        setForbidden(true);
        setUsers([]);
        return;
      }
      setError((err as Error).message);
      setUsers([]);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const remove = async (u: AdminUser) => {
    const total = u.uploadCount + u.txnCount + u.ruleCount + u.categoryCount;
    if (
      !window.confirm(
        `Permanently delete ${u.email} and all ${total} associated records ` +
          `(${u.txnCount} transactions, ${u.uploadCount} uploads, ` +
          `${u.ruleCount} rules, ${u.categoryCount} custom categories)?\n\n` +
          "This cannot be undone.",
      )
    ) {
      return;
    }
    setDeleting(u.id);
    try {
      await deleteAdminUser(u.id);
      await load();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setDeleting("");
    }
  };

  return (
    <div className="container">
      <header className="topbar">
        <h1>Administration</h1>
        <span className="tagline">accounts and their data</span>
        <span className="session-info">
          <button className="link-btn" onClick={onBack}>
            ← Back to dashboard
          </button>
        </span>
      </header>

      {forbidden && (
        <div className="card" style={{ color: "var(--delta-bad)" }}>
          Your account ({currentUser.email}) is not an administrator. Ask an
          existing administrator to add your address to <code>ADMIN_EMAILS</code>.
        </div>
      )}

      {error && (
        <div className="card" style={{ color: "var(--delta-bad)" }}>
          {error}
        </div>
      )}

      {!forbidden && (
        <div className="card">
          <div className="card-header-row">
            <h2>Users{users ? ` (${users.length})` : ""}</h2>
            <button className="link-btn" onClick={() => void load()}>
              Refresh
            </button>
          </div>

          {users === null ? (
            <div className="empty">Loading…</div>
          ) : users.length === 0 ? (
            <div className="empty">No accounts found.</div>
          ) : (
            <div className="table-scroll">
              <table className="txns">
                <thead>
                  <tr>
                    <th>Email</th>
                    <th>Joined</th>
                    <th className="amt">Transactions</th>
                    <th className="amt">Uploads</th>
                    <th className="amt">Rules</th>
                    <th className="amt">Categories</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {users.map((u) => {
                    const isSelf = u.id === currentUser.id;
                    return (
                      <tr key={u.id}>
                        <td>
                          {u.email}
                          {isSelf && <span className="status-chip"> you</span>}
                          {u.isAdmin && <span className="status-chip"> admin</span>}
                        </td>
                        <td>{new Date(u.createdAt).toLocaleDateString()}</td>
                        <td className="amt">{u.txnCount}</td>
                        <td className="amt">{u.uploadCount}</td>
                        <td className="amt">{u.ruleCount}</td>
                        <td className="amt">{u.categoryCount}</td>
                        <td className="amt">
                          <button
                            className="danger-btn"
                            /* Deleting your own account would revoke the
                               session you're using; the server refuses it too. */
                            disabled={isSelf || deleting === u.id}
                            title={
                              isSelf
                                ? "You cannot delete your own account"
                                : `Delete ${u.email} and all their data`
                            }
                            onClick={() => void remove(u)}
                          >
                            {deleting === u.id ? "Deleting…" : "Delete"}
                          </button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
