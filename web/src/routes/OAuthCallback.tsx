import { useEffect, useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useAuth } from "../state/AuthContext";
import { Spinner } from "../components/ui";

/** Landing point for go-login's success redirect: /oauth/callback?token=… */
export function OAuthCallback() {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const { loginWithToken } = useAuth();
  const [error, setError] = useState("");
  const ran = useRef(false);

  useEffect(() => {
    if (ran.current) return;
    ran.current = true;
    const token = params.get("token");
    if (!token) {
      navigate("/login?error=Sign+in+failed", { replace: true });
      return;
    }
    loginWithToken(token)
      .then(() => navigate("/app", { replace: true }))
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
