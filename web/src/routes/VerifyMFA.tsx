import { useState } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { api } from "../lib/api";
import { useAuth } from "../state/AuthContext";
import { AuthShell } from "./AuthShell";
import { ErrorText } from "../components/ui";

export function rememberMFA(challenge: string) {
  try {
    sessionStorage.setItem("lifeai_mfa", challenge);
  } catch {
    /* Router state remains available. */
  }
}
export function loginDestination() {
  try {
    const next = sessionStorage.getItem("lifeai_auth_return");
    sessionStorage.removeItem("lifeai_auth_return");
    if (next === "/app/settings#security") return next;
  } catch {
    /* Default to the dashboard. */
  }
  return "/app";
}

export function VerifyMFA() {
  const location = useLocation();
  const navigate = useNavigate();
  const { loginWithToken } = useAuth();
  const [challenge] = useState(() => {
    if (location.state?.challenge) return String(location.state.challenge);
    try {
      return sessionStorage.getItem("lifeai_mfa") || "";
    } catch {
      return "";
    }
  });
  const [code, setCode] = useState("");
  const [recovery, setRecovery] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      const result = await api.verifyMFA(challenge, code);
      await loginWithToken(result.token);
      try {
        sessionStorage.removeItem("lifeai_mfa");
      } catch {
        /* Nothing persisted. */
      }
      navigate(loginDestination(), { replace: true });
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Could not verify your code.",
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <AuthShell
      title="One more step"
      subtitle={
        recovery
          ? "Use one of your saved recovery codes"
          : "Open your authenticator app"
      }
    >
      {challenge ? (
        <form onSubmit={submit} className="space-y-4">
          <label className="block">
            <span className="label">
              {recovery ? "Recovery code" : "Authentication code"}
            </span>
            <input
              className="field font-mono text-lg tracking-widest"
              autoFocus
              autoComplete="one-time-code"
              inputMode={recovery ? "text" : "numeric"}
              maxLength={recovery ? 16 : 6}
              required
              value={code}
              onChange={(event) => setCode(event.target.value)}
            />
          </label>
          <ErrorText>{error}</ErrorText>
          <button className="btn-primary w-full" disabled={busy}>
            {busy ? "Verifying…" : "Verify and sign in"}
          </button>
          <button
            type="button"
            className="btn-ghost w-full"
            onClick={() => {
              setRecovery(!recovery);
              setCode("");
              setError("");
            }}
          >
            {recovery ? "Use authenticator app" : "Use a recovery code"}
          </button>
          <p className="text-xs text-ink-500">
            Codes can be used once. This sign-in attempt expires after five
            minutes.
          </p>
        </form>
      ) : (
        <p className="text-sm text-ink-500">
          Start a new sign-in attempt to verify your identity.
        </p>
      )}
      <Link className="mt-5 block text-center text-sm text-ink-500" to="/login">
        Back to sign in
      </Link>
    </AuthShell>
  );
}
