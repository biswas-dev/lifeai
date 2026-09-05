import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { api } from "../lib/api";
import type { AuthConfig } from "../lib/types";
import { useAuth } from "../state/AuthContext";
import { AuthShell, OAuthButtons } from "./AuthShell";
import { ErrorText } from "../components/ui";

export function Signup() {
  const { signup } = useAuth();
  const navigate = useNavigate();
  const [name, setName] = useState("");
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
  }, []);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await signup(email, password, name);
      navigate("/app/settings?welcome=1", { replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not create account");
    } finally {
      setBusy(false);
    }
  }

  if (config && !config.allow_signup) {
    return (
      <AuthShell
        title="Registration closed"
        subtitle="This instance is not accepting new accounts"
      >
        <Link to="/login" className="btn-ghost w-full">
          Back to sign in
        </Link>
      </AuthShell>
    );
  }

  return (
    <AuthShell title="Create your account" subtitle="Free. Private. Yours.">
      <form onSubmit={submit} className="space-y-4">
        <label className="block">
          <span className="label">Name</span>
          <input
            className="field"
            autoComplete="name"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </label>
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
            autoComplete="new-password"
            minLength={8}
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
          <span className="mt-1 block text-xs text-ink-500">
            At least 8 characters.
          </span>
        </label>
        <ErrorText>{error}</ErrorText>
        <button className="btn-primary w-full" disabled={busy}>
          {busy ? "Creating…" : "Create account"}
        </button>
        <p className="text-center text-sm text-ink-500">
          Already have one?{" "}
          <Link to="/login" className="text-vital-400 hover:text-vital-300">
            Sign in
          </Link>
        </p>
      </form>
      <OAuthButtons config={config} />
    </AuthShell>
  );
}
