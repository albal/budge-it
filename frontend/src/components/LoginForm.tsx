import { useState } from "react";
import { login } from "../api";
import type { User } from "../types";

export default function LoginForm({ onLoggedIn }: { onLoggedIn: (user: User) => void }) {
  const [email, setEmail] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      onLoggedIn(await login(email));
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="login-screen">
      <form className="card login-card" onSubmit={submit}>
        <h1>Budge-it</h1>
        <p className="tagline">know where your money goes</p>
        <label htmlFor="login-email">Email</label>
        <input
          id="login-email"
          type="email"
          required
          autoFocus
          placeholder="you@example.com"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
        />
        {error && <div className="login-error">{error}</div>}
        <button type="submit" disabled={busy || !email}>
          {busy ? "Signing in…" : "Continue"}
        </button>
        <p className="hint">No password needed — we'll create an account if you're new.</p>
      </form>
    </div>
  );
}
