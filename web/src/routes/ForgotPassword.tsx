import { useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { api } from "../lib/api";
import { useAuth } from "../state/AuthContext";
import { AuthShell } from "./AuthShell";
import { ErrorText } from "../components/ui";

export function ForgotPassword() {
  const [email, setEmail] = useState("");
  const [message, setMessage] = useState("");
  const [resetUrl, setResetUrl] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const res = await api.forgotPassword(email);
      setMessage(res.message);
      if (res.reset_url) setResetUrl(res.reset_url);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not start a reset");
    } finally {
      setBusy(false);
    }
  }

  return (
    <AuthShell
      title="Reset your password"
      subtitle="Recover access to your account"
    >
      {message ? (
        <div className="space-y-4 text-sm text-ink-300">
          <p>{message}</p>
          {resetUrl && (
            <p className="break-all rounded-xl bg-ink-850 p-3 text-xs">
              <a className="text-vital-400" href={resetUrl}>
                {resetUrl}
              </a>
            </p>
          )}
          <p className="text-xs text-ink-500">
            This recovery link expires after one hour.
          </p>
          <Link to="/login" className="btn-ghost w-full">
            Back to sign in
          </Link>
        </div>
      ) : (
        <form onSubmit={submit} className="space-y-4">
          <label className="block">
            <span className="label">Email</span>
            <input
              className="field"
              type="email"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />
          </label>
          <ErrorText>{error}</ErrorText>
          <button className="btn-primary w-full" disabled={busy}>
            {busy ? "Working…" : "Request recovery"}
          </button>
          <p className="text-center text-sm">
            <Link to="/login" className="text-ink-500 hover:text-ink-300">
              Back to sign in
            </Link>
          </p>
        </form>
      )}
    </AuthShell>
  );
}

export function ResetPassword() {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const { loginWithToken } = useAuth();
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const token = params.get("token") || "";

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const res = await api.resetPassword(token, password);
      await loginWithToken(res.token);
      navigate("/app", { replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "That link is not valid");
    } finally {
      setBusy(false);
    }
  }

  return (
    <AuthShell
      title="Choose a new password"
      subtitle="Then you'll be signed straight in"
    >
      <form onSubmit={submit} className="space-y-4">
        <label className="block">
          <span className="label">New password</span>
          <input
            className="field"
            type="password"
            autoComplete="new-password"
            minLength={8}
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </label>
        <ErrorText>{error}</ErrorText>
        <button className="btn-primary w-full" disabled={busy || !token}>
          {busy ? "Saving…" : "Set password"}
        </button>
      </form>
    </AuthShell>
  );
}
