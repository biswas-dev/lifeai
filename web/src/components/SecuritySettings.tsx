import { useEffect, useState } from "react";
import QRCode from "qrcode";
import { api, ApiError } from "../lib/api";
import type { AuthConfig, SecurityResult, SecurityStatus } from "../lib/types";
import {
  passkeysSupported,
  registerPasskey,
  signInWithPasskey,
} from "../lib/passkeys";
import { useAuth } from "../state/AuthContext";
import { ErrorText } from "./ui";

export function SecuritySettings() {
  const { user, loginWithToken } = useAuth();
  const [status, setStatus] = useState<SecurityStatus | null>(null);
  const [config, setConfig] = useState<AuthConfig | null>(null);
  const [setup, setSetup] = useState<{
    challenge: string;
    secret: string;
    uri: string;
  } | null>(null);
  const [qr, setQR] = useState("");
  const [code, setCode] = useState("");
  const [password, setPassword] = useState("");
  const [reauthCode, setReauthCode] = useState("");
  const [name, setName] = useState("");
  const [codes, setCodes] = useState<string[]>([]);
  const [saved, setSaved] = useState(false);
  const [confirm, setConfirm] = useState<
    "disable" | "regenerate" | string | null
  >(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  async function reload() {
    setStatus(await api.securityStatus());
  }
  useEffect(() => {
    reload().catch((err) => setError(err.message));
    api
      .authConfig()
      .then(setConfig)
      .catch(() => setConfig(null));
  }, []);
  useEffect(() => {
    let active = true;
    setQR("");
    if (setup)
      QRCode.toDataURL(setup.uri, { width: 216, margin: 2 })
        .then((url) => {
          if (active) setQR(url);
        })
        .catch(() => {
          /* Manual setup remains available. */
        });
    return () => {
      active = false;
    };
  }, [setup]);

  async function run(action: () => Promise<void>) {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await action();
    } catch (err) {
      setError(
        err instanceof Error
          ? err.message
          : "Could not update account security.",
      );
      if (err instanceof ApiError && err.code === "reauth_required")
        setStatus(
          (previous) =>
            previous && { ...previous, recent_authentication: false },
        );
    } finally {
      setBusy(false);
    }
  }
  async function accept(result: SecurityResult) {
    await loginWithToken(result.token);
    if (result.recovery_codes) {
      setCodes(result.recovery_codes);
      setSaved(false);
    }
    await reload();
  }
  function downloadCodes() {
    const body = `Lifeai recovery codes\nAccount: ${user?.email}\nSite: ${window.location.origin}\nEach code can be used once. Keep these somewhere private.\n\n${codes.join("\n")}\n`;
    const url = URL.createObjectURL(new Blob([body], { type: "text/plain" }));
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = "lifeai-recovery-codes.txt";
    anchor.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
  const fresh = status?.recent_authentication;

  return (
    <section id="security" className="card scroll-mt-5 space-y-6 p-5 sm:p-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="eyebrow mb-2">Yours to protect</p>
          <h2 className="text-xl font-semibold">Account security</h2>
        </div>
        {status && (
          <span className="chip">
            {status.totp_enabled ? "2FA enabled" : "2FA not enabled"}
          </span>
        )}
      </div>
      <p className="text-sm leading-6 text-ink-500">
        Protect your record with an authenticator app and sign in with a passkey
        from your phone, computer, or password manager.
      </p>
      <ErrorText>{error}</ErrorText>
      {notice && (
        <p role="status" className="text-sm text-vital-500">
          {notice}
        </p>
      )}
      {!status && !error && (
        <p className="text-sm text-ink-500">Loading security settings…</p>
      )}

      {status && !fresh && (
        <div className="rounded-2xl border border-ink-800 bg-ink-950 p-4">
          <h3 className="font-medium">Confirm it’s you</h3>
          <p className="mt-1 text-sm text-ink-500">
            Verify your identity to change security settings. Verification lasts
            five minutes.
          </p>
          <form
            className="mt-4 space-y-3"
            onSubmit={(event) => {
              event.preventDefault();
              void run(async () => {
                await accept(await api.reauthenticate(password, reauthCode));
                setPassword("");
                setReauthCode("");
                setNotice("Identity confirmed.");
              });
            }}
          >
            <label className="block">
              <span className="label">Current password</span>
              <input
                type="password"
                autoComplete="current-password"
                className="field"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                required
              />
            </label>
            {status.totp_enabled && (
              <label className="block">
                <span className="label">Authenticator or recovery code</span>
                <input
                  className="field font-mono"
                  autoComplete="one-time-code"
                  value={reauthCode}
                  onChange={(event) => setReauthCode(event.target.value)}
                  required
                  maxLength={16}
                />
              </label>
            )}
            <button className="btn-primary" disabled={busy}>
              Confirm identity
            </button>
          </form>
          <div className="mt-3 flex flex-wrap gap-3">
            {config?.google && (
              <a
                className="btn-ghost"
                href="/api/auth/google"
                onClick={() => {
                  try {
                    sessionStorage.setItem(
                      "lifeai_auth_return",
                      "/app/settings#security",
                    );
                  } catch {
                    /* Can return to Settings manually. */
                  }
                }}
              >
                Verify with Google
              </a>
            )}
            {status.passkeys.length > 0 && passkeysSupported() && (
              <button
                className="btn-ghost"
                disabled={busy}
                onClick={() =>
                  void run(async () => {
                    const result = await signInWithPasskey();
                    if (result.user.id !== user?.id)
                      throw new Error(
                        "Choose a passkey for the account you are currently using.",
                      );
                    await accept(result);
                    setNotice("Identity confirmed with your passkey.");
                  })
                }
              >
                Verify with a passkey
              </button>
            )}
          </div>
        </div>
      )}

      {codes.length > 0 && (
        <div className="space-y-4 rounded-2xl border border-vital-500/30 bg-vital-500/5 p-5">
          <h3 className="font-semibold">Save your recovery codes</h3>
          <p className="text-sm text-ink-500">
            These codes are shown once. Each replaces one authenticator code if
            you lose access to your app. Store them somewhere private outside
            Lifeai.
          </p>
          <div className="grid grid-cols-1 gap-2 font-mono text-sm sm:grid-cols-2">
            {codes.map((item) => (
              <code key={item} className="rounded-lg bg-white px-3 py-2">
                {item}
              </code>
            ))}
          </div>
          <button type="button" className="btn-ghost" onClick={downloadCodes}>
            Download recovery codes
          </button>
          <label className="flex items-start gap-3 text-sm">
            <input
              type="checkbox"
              checked={saved}
              onChange={(event) => setSaved(event.target.checked)}
              className="mt-1"
            />
            I’ve saved these codes somewhere safe.
          </label>
          <button
            type="button"
            className="btn-primary"
            disabled={!saved}
            onClick={() => {
              setCodes([]);
              setSaved(false);
            }}
          >
            Done
          </button>
        </div>
      )}

      {status && (
        <div className="space-y-4 border-t border-ink-800 pt-5">
          <div>
            <h3 className="font-semibold">Authenticator app</h3>
            <p className="mt-1 text-sm leading-6 text-ink-500">
              When enabled, password and Google sign-ins also ask for a
              six-digit code. Works with Google Authenticator, Microsoft
              Authenticator, 1Password, and other TOTP apps.
            </p>
          </div>
          {!status.totp_available && (
            <p className="text-sm text-ink-500">
              Authenticator setup is unavailable on this server.
            </p>
          )}
          {status.totp_enabled ? (
            <>
              <p className="text-sm text-ink-500">
                {status.recovery_codes_remaining} recovery codes remaining.
              </p>
              <div className="flex flex-wrap gap-3">
                <button
                  className="btn-ghost"
                  disabled={busy || !fresh || codes.length > 0}
                  onClick={() => setConfirm("regenerate")}
                >
                  Generate new recovery codes
                </button>
                <button
                  className="btn-ghost"
                  disabled={busy || !fresh || codes.length > 0}
                  onClick={() => setConfirm("disable")}
                >
                  Turn off 2FA
                </button>
              </div>
            </>
          ) : setup ? (
            <form
              className="space-y-4 rounded-2xl border border-ink-800 p-4"
              onSubmit={(event) => {
                event.preventDefault();
                void run(async () => {
                  await accept(await api.confirmTOTP(setup.challenge, code));
                  setSetup(null);
                  setCode("");
                  setNotice(
                    "Two-factor authentication is enabled. Other browser sessions have been signed out.",
                  );
                });
              }}
            >
              <p className="text-sm">
                Scan this QR code with your authenticator app, then enter its
                current code.
              </p>
              {qr && (
                <img
                  src={qr}
                  width={216}
                  height={216}
                  alt="Authenticator setup QR code"
                  className="rounded-xl"
                />
              )}
              <details className="text-sm">
                <summary className="cursor-pointer text-vital-500">
                  Can’t scan? Use the setup key
                </summary>
                <code className="mt-3 block break-all rounded-xl bg-ink-950 p-3 font-mono">
                  {setup.secret}
                </code>
                <p className="mt-2 text-xs text-ink-500">
                  Time-based · 6 digits · 30 seconds · SHA1
                </p>
              </details>
              <label className="block">
                <span className="label">Six-digit authentication code</span>
                <input
                  className="field max-w-xs font-mono tracking-widest"
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  required
                  pattern="[0-9]{6}"
                  maxLength={6}
                  value={code}
                  onChange={(event) => setCode(event.target.value)}
                />
              </label>
              <div className="flex flex-wrap gap-3">
                <button className="btn-primary" disabled={busy || !fresh}>
                  Verify and enable 2FA
                </button>
                <button
                  type="button"
                  className="btn-ghost"
                  disabled={busy}
                  onClick={() => {
                    setSetup(null);
                    setCode("");
                  }}
                >
                  Cancel setup
                </button>
              </div>
              <p className="text-xs text-ink-500">
                2FA stays off until the code is confirmed. Setup expires after
                five minutes.
              </p>
            </form>
          ) : (
            <button
              className="btn-primary"
              disabled={busy || !fresh || !status.totp_available}
              onClick={() =>
                void run(async () => {
                  setSetup(await api.beginTOTP());
                  setCode("");
                })
              }
            >
              Set up authenticator
            </button>
          )}
        </div>
      )}

      {status && (
        <div className="space-y-4 border-t border-ink-800 pt-5">
          <div>
            <h3 className="font-semibold">Passkeys</h3>
            <p className="mt-1 text-sm leading-6 text-ink-500">
              Sign in with your device’s PIN, fingerprint, or face recognition.
              A verified passkey satisfies 2FA without an extra authenticator
              code. Passkeys are tied to this website.
            </p>
          </div>
          {status.passkeys.length ? (
            <ul className="divide-y divide-ink-800">
              {status.passkeys.map((key) => (
                <li
                  className="flex flex-wrap items-center justify-between gap-3 py-3"
                  key={key.id}
                >
                  <div>
                    <p className="text-sm font-medium">{key.name}</p>
                    <p className="mt-1 text-xs text-ink-500">
                      Added {key.created_at.split(" ")[0]} ·{" "}
                      {key.last_used_at
                        ? `Last used ${key.last_used_at.split(" ")[0]}`
                        : "Not used yet"}
                      {key.backed_up ? " · Backed up by your provider" : ""}
                    </p>
                  </div>
                  <button
                    className="btn-ghost text-xs"
                    disabled={busy || !fresh}
                    aria-label={`Remove ${key.name}`}
                    onClick={() => setConfirm(key.id)}
                  >
                    Remove
                  </button>
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-sm text-ink-500">
              No passkeys yet. Add one to make your next sign-in easier.
            </p>
          )}
          {status.passkeys_available && passkeysSupported() ? (
            <form
              className="flex flex-wrap items-end gap-3"
              onSubmit={(event) => {
                event.preventDefault();
                void run(async () => {
                  await accept(await registerPasskey(name));
                  setName("");
                  setNotice(
                    "Passkey added. Other browser sessions have been signed out.",
                  );
                });
              }}
            >
              <label className="min-w-0 flex-1">
                <span className="label">Passkey name</span>
                <input
                  className="field"
                  placeholder="e.g. Personal MacBook"
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                  maxLength={80}
                  required
                />
              </label>
              <button className="btn-primary" disabled={busy || !fresh}>
                Add passkey
              </button>
            </form>
          ) : (
            <p className="text-sm text-ink-500">
              Use an up-to-date browser over HTTPS to add a passkey.
            </p>
          )}
        </div>
      )}

      {confirm && (
        <div
          className="space-y-3 rounded-xl border border-ink-700 p-4"
          role="alert"
        >
          <p className="text-sm">
            {confirm === "disable"
              ? "Turn off authenticator 2FA? Your recovery codes will be removed. Your passkeys will keep working."
              : confirm === "regenerate"
                ? "Replace all recovery codes? Previously saved codes will stop working."
                : "Remove this passkey from Lifeai? You can still use your other sign-in methods. Remove its saved copy from your device separately if you no longer need it."}
          </p>
          <div className="flex gap-3">
            <button
              className="btn-primary"
              disabled={busy || !fresh}
              onClick={() =>
                void run(async () => {
                  await accept(
                    confirm === "disable"
                      ? await api.disableTOTP()
                      : confirm === "regenerate"
                        ? await api.regenerateRecoveryCodes()
                        : await api.deletePasskey(confirm),
                  );
                  setConfirm(null);
                  setNotice(
                    "Security settings updated. Other browser sessions have been signed out.",
                  );
                })
              }
            >
              Confirm change
            </button>
            <button
              className="btn-ghost"
              disabled={busy}
              onClick={() => setConfirm(null)}
            >
              Cancel
            </button>
          </div>
        </div>
      )}
    </section>
  );
}
