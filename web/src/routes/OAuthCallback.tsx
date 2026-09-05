import { useEffect, useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useAuth } from "../state/AuthContext";
import { Spinner } from "../components/ui";
import { rememberMFA, loginDestination } from "./VerifyMFA";

/** Consume the session fragment, then immediately remove it from history. */
export function OAuthCallback() {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const { loginWithToken } = useAuth();
  const [error, setError] = useState("");
  const ran = useRef(false);

  useEffect(() => {
    if (ran.current) return;
    ran.current = true;
    const fragment = new URLSearchParams(window.location.hash.slice(1));
    const token = fragment.get("token") || params.get("token");
    const challenge = fragment.get("challenge");
    window.history.replaceState(null, "", window.location.pathname);
    if (challenge) {
      rememberMFA(challenge);
      navigate("/verify", { state: { challenge }, replace: true });
      return;
    }
    if (!token) {
      navigate("/login?error=Sign+in+failed", { replace: true });
      return;
    }
    loginWithToken(token)
      .then(() => navigate(loginDestination(), { replace: true }))
      .catch(() => setError("That sign-in link was not valid."));
  }, [params, loginWithToken, navigate]);

  return (
    <div className="flex min-h-dvh flex-col items-center justify-center gap-4 px-4">
      {error ? (
        <>
          <p className="text-center text-rose-400">{error}</p>
          <button
            className="btn-ghost"
            onClick={() => navigate("/login", { replace: true })}
          >
            Back to sign in
          </button>
        </>
      ) : (
        <>
          <Spinner className="h-8 w-8" />
          <p className="text-sm text-ink-500">Signing you in…</p>
        </>
      )}
    </div>
  );
}
