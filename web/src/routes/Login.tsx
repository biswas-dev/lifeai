import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { api } from "../lib/api";
import type { AuthConfig } from "../lib/types";
import { useAuth } from "../state/AuthContext";
import { AuthShell, OAuthButtons } from "./AuthShell";
import { ErrorText } from "../components/ui";

export function Login() {
  const { login } = useAuth();
  const navigate = useNavigate();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [config, setConfig] = useState<AuthConfig | null>(null);

  useEffect(() => {
    api
      .authConfig()
      .then(setConfig)
      .catch(() => setConfig(null));
    const reason = new URLSearchParams(window.location.search).get("error");
    if (reason) setError(reason);
  }, []);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await login(email, password);
      navigate("/app", { replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Sign in failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <AuthShell title="Welcome back" subtitle="Sign in to your record">
      <form onSubmit={submit} className="space-y-4">
        <label className="block">
          <span className="label">Email</span>
          <input
            className="field"
            type="email"
            autoComplete="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
        </label>
        <label className="block">
          <span className="label">Password</span>
          <input
            className="field"
            type="password"
            autoComplete="current-password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </label>
        <ErrorText>{error}</ErrorText>
        <button className="btn-primary w-full" disabled={busy}>
          {busy ? "Signing in…" : "Sign in"}
        </button>
        <div className="flex justify-between text-sm">
          <Link
            to="/forgot-password"
            className="text-ink-500 hover:text-ink-300"
          >
            Forgot password?
          </Link>
          {config?.allow_signup !== false && (
            <Link to="/signup" className="text-vital-400 hover:text-vital-300">
              Create account
            </Link>
          )}
        </div>
      </form>
      <OAuthButtons config={config} />
    </AuthShell>
  );
}
