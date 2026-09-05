import { useEffect, useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { api } from "../lib/api";
import type { Goals, ImportSummary } from "../lib/types";
import { useAuth } from "../state/AuthContext";
import { message, useResource } from "../lib/useResource";
import { timeAgo } from "../lib/format";
import { ErrorText, Field, PageHeader } from "../components/ui";

function Section({
  title,
  children,
  id,
}: {
  title: string;
  children: ReactNode;
  id?: string;
}) {
  return (
    <section className="card scroll-mt-5 space-y-4 p-5" id={id}>
      <h2 className="font-semibold text-ink-100">{title}</h2>
      {children}
    </section>
  );
}
function useAction() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [done, setDone] = useState("");
  async function run(fn: () => Promise<unknown>, success = "Saved") {
    setBusy(true);
    setError("");
    setDone("");
    try {
      await fn();
      setDone(success);
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }
  return {
    busy,
    run,
    feedback: (
      <>
        <ErrorText>{error}</ErrorText>
        {done && (
          <p role="status" className="text-sm text-vital-400">
            {done}
          </p>
        )}
      </>
    ),
  };
}
function ProfileSettings() {
  const { user, refresh } = useAuth();
  const a = useAction();
  const [name, setName] = useState(user?.name || "");
  const [dob, setDob] = useState(user?.dob || "");
  const [sex, setSex] = useState(user?.sex || "");
  const [height, setHeight] = useState(user?.height_cm?.toString() || "");
  const [timezone, setTimezone] = useState(user?.timezone || "UTC");
  const [unit, setUnit] = useState<"kg" | "lb">(user?.weight_unit || "kg");
  return (
    <Section title="Your profile">
      <p className="text-sm text-ink-500">{user?.email}</p>
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          void a.run(async () => {
            await api.updateProfile({
              name,
              dob,
              sex,
              height_cm: height ? Number(height) : 0,
              timezone,
              weight_unit: unit,
            });
            await refresh();
          });
        }}
      >
        <Field label="Name">
          <input
            className="field"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label="Date of birth">
            <input
              className="field"
              type="date"
              value={dob}
              onChange={(e) => setDob(e.target.value)}
            />
          </Field>
          <Field label="Sex">
            <select
              className="field"
              value={sex}
              onChange={(e) => setSex(e.target.value)}
            >
              <option value="">Not specified</option>
              <option value="male">Male</option>
              <option value="female">Female</option>
              <option value="other">Other</option>
            </select>
          </Field>
          <Field label="Height (cm)" hint="5 ft 9 in = 175.26 cm">
            <input
              type="number"
              min="50"
              max="272"
              step="0.01"
              className="field"
              value={height}
              onChange={(e) => setHeight(e.target.value)}
            />
          </Field>
          <Field label="Weight units">
            <select
              className="field"
              value={unit}
              onChange={(e) => setUnit(e.target.value as "kg" | "lb")}
            >
              <option value="kg">Kilograms</option>
              <option value="lb">Pounds</option>
            </select>
          </Field>
        </div>
        <Field label="Timezone">
          <input
            className="field"
            required
            value={timezone}
            onChange={(e) => setTimezone(e.target.value)}
          />
        </Field>
        {a.feedback}
        <button className="btn-primary" disabled={a.busy}>
          Save profile
        </button>
      </form>
    </Section>
  );
}
function GoalSettings() {
  const result = useResource(() => api.goals());
  const [goals, setGoals] = useState<Goals | null>(null);
  const a = useAction();
  useEffect(() => {
    if (result.data) setGoals(result.data);
  }, [result.data]);
  const fields = [
    ["daily_kcal", "Calories (kcal)"],
    ["protein_g", "Protein (g)"],
    ["carbs_g", "Carbs (g)"],
    ["fat_g", "Fat (g)"],
    ["target_weight_kg", "Target weight (kg)"],
    ["steps", "Daily steps"],
    ["water_ml", "Water (ml)"],
    ["sleep_hours", "Sleep (hours)"],
    ["workout_minutes", "Training (minutes)"],
  ] as const;
  return (
    <Section title="Your daily goals">
      <p className="text-sm text-ink-400">
        Set targets that fit your plan. Leave a field empty if you do not want a
        target.
      </p>
      <ErrorText>{result.error}</ErrorText>
      {goals && (
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            void a.run(() => api.saveGoals(goals));
          }}
        >
          <div className="grid grid-cols-2 gap-3">
            {fields.map(([key, label]) => (
              <Field key={key} label={label}>
                <input
                  type="number"
                  className="field"
                  min="0"
                  step={
                    key === "sleep_hours" || key === "target_weight_kg"
                      ? "0.1"
                      : "1"
                  }
                  value={goals[key] ?? ""}
                  onChange={(e) =>
                    setGoals({
                      ...goals,
                      [key]:
                        e.target.value === "" ? null : Number(e.target.value),
                    })
                  }
                />
              </Field>
            ))}
          </div>
          <Field label="Plan & preferences">
            <textarea
              className="field"
              rows={3}
              value={goals.notes}
              placeholder="Food preferences, training focus, and goals agreed with your clinician"
              onChange={(e) => setGoals({ ...goals, notes: e.target.value })}
            />
          </Field>
          {a.feedback}
          <button className="btn-primary" disabled={a.busy}>
            Save goals
          </button>
        </form>
      )}
    </Section>
  );
}
function Hard75Settings() {
  const status = useResource(() => api.hard75Status());
  const a = useAction();
  const [token, setToken] = useState("");
  const st = status.data;
  useEffect(() => {
    if (!st?.running) return;
    const t = setInterval(status.reload, 4000);
    return () => clearInterval(t);
  }, [st?.running, status.reload]);
  return (
    <Section title="75hard · your private bridge">
      <p className="text-sm text-ink-400">
        Pull photos, food, metrics, workouts, meditation and journal once every
        24 hours. This connection only imports into lifeai.
      </p>
      <ErrorText>{status.error}</ErrorText>
      {st && (
        <>
          <p className="text-xs text-ink-500">
            {st.connected
              ? `Connected · last pull ${timeAgo(st.last_sync_at)} · ${st.last_status || "waiting"}`
              : "Not connected"}
          </p>
          <ErrorText>{st.last_error}</ErrorText>
          {st.last_summary && (
            <p className="text-xs text-ink-400">
              Last pull: {st.last_summary.days} days, {st.last_summary.photos}{" "}
              photos, {st.last_summary.meals} meals.
            </p>
          )}
          {!st.connected ? (
            <form
              className="space-y-3"
              onSubmit={(e) => {
                e.preventDefault();
                void a.run(async () => {
                  await api.hard75Connect({ token, enabled: true });
                  setToken("");
                  status.reload();
                }, "Connected. Use Pull now to start the first import.");
              }}
            >
              <Field label="75hard read-only API token">
                <input
                  type="password"
                  autoComplete="off"
                  className="field"
                  required
                  value={token}
                  onChange={(e) => setToken(e.target.value)}
                />
              </Field>
              <button
                className="btn-primary"
                disabled={a.busy || !st.can_store}
              >
                Connect 75hard
              </button>
            </form>
          ) : (
            <div className="flex flex-wrap gap-2">
              <button
                className="btn-primary"
                disabled={a.busy || st.running || !st.enabled}
                onClick={() =>
                  void a.run(async () => {
                    await api.hard75Sync();
                    status.reload();
                  }, "Pull started. Status updates here.")
                }
              >
                {st.running ? "Pulling…" : "Pull now"}
              </button>
              <button
                className="btn-ghost"
                disabled={a.busy}
                onClick={() =>
                  void a.run(async () => {
                    await api.hard75Connect({ enabled: !st.enabled });
                    status.reload();
                  })
                }
              >
                {st.enabled ? "Pause daily pulls" : "Resume daily pulls"}
              </button>
              <button
                className="btn-danger"
                disabled={a.busy}
                onClick={() => {
                  if (
                    window.confirm(
                      "Disconnect 75hard? Your imported history will remain.",
                    )
                  )
                    void a.run(async () => {
                      await api.hard75Disconnect();
                      status.reload();
                    }, "Disconnected");
                }}
              >
                Disconnect
              </button>
            </div>
          )}
        </>
      )}
      {a.feedback}
    </Section>
  );
}
function HealthImports() {
  const strava = useResource(() => api.stravaStatus());
  const imports = useResource(() => api.importRuns());
  const a = useAction();
  const [summary, setSummary] = useState<ImportSummary | null>(null);
  return (
    <Section title="Health connections" id="integrations">
      <ErrorText>{strava.error || imports.error}</ErrorText>
      <div>
        <h3 className="text-sm font-medium">Strava</h3>
        <p className="mt-1 text-xs text-ink-500">
          {strava.data?.connected
            ? `Connected as ${strava.data.username || strava.data.athlete_id} · last sync ${timeAgo(strava.data.last_sync_at)}`
            : strava.data?.configured
              ? "Connect your Strava account to import activities automatically."
              : "A Strava application must be configured on this server before you can connect."}
        </p>
        <ErrorText>{strava.data?.last_error}</ErrorText>
        <div className="mt-3 flex gap-2">
          {strava.data?.connected ? (
            <>
              <button
                disabled={a.busy}
                className="btn-ghost"
                onClick={() =>
                  void a.run(async () => {
                    setSummary(await api.stravaSync());
                    strava.reload();
                    imports.reload();
                  }, "Strava sync completed")
                }
              >
                Sync now
              </button>
              <button
                className="btn-danger"
                disabled={a.busy}
                onClick={() => {
                  if (
                    window.confirm(
                      "Disconnect Strava? Imported workouts will remain.",
                    )
                  )
                    void a.run(async () => {
                      await api.stravaDisconnect();
                      strava.reload();
                    }, "Disconnected");
                }}
              >
                Disconnect
              </button>
            </>
          ) : (
            <button
              className="btn-ghost"
              disabled={a.busy || !strava.data?.configured}
              onClick={() =>
                void a.run(async () => {
                  const { url } = await api.stravaConnect();
                  window.location.assign(url);
                }, "")
              }
            >
              Connect Strava
            </button>
          )}
        </div>
      </div>
      <div className="border-t border-ink-800 pt-4">
        <h3 className="text-sm font-medium">Apple Health & Samsung Health</h3>
        <p className="mt-2 text-sm text-ink-400">
          Upload your exported data: Apple export.zip or export.xml, or a
          Samsung Health ZIP. Matching workout times and durations are
          deduplicated across sources. Daily metrics use source precedence;
          values you type take priority.
        </p>
        <div className="mt-3 grid gap-4 sm:grid-cols-2">
          {(["apple", "samsung"] as const).map((source) => (
            <Field
              key={source}
              label={
                source === "apple"
                  ? "Apple Health export"
                  : "Samsung Health export"
              }
            >
              <input
                className="block w-full text-xs text-ink-400 file:mr-2 file:rounded-lg file:border-0 file:bg-ink-800 file:p-2 file:text-ink-200"
                type="file"
                accept={source === "apple" ? ".zip,.xml" : ".zip"}
                disabled={a.busy}
                onChange={(e) => {
                  const file = e.target.files?.[0];
                  if (file)
                    void a.run(async () => {
                      setSummary(await api.importHealth(source, file));
                      imports.reload();
                    }, "Import completed");
                }}
              />
            </Field>
          ))}
        </div>
        <p className="mt-3 text-xs text-ink-500">
          Automatic phone uploads can send JSON to /api/import/health using a
          read + write API token. File uploads may also be limited by your
          network proxy.
        </p>
      </div>
      {a.busy && (
        <p role="status" className="text-sm text-ink-400">
          Working… Large exports can take a few minutes.
        </p>
      )}
      {a.feedback}
      {summary && (
        <p className="text-sm text-vital-400">
          {summary.samples} readings · {summary.workouts} new workouts ·{" "}
          {summary.workouts_skipped} matched workouts · {summary.days_touched}{" "}
          days
        </p>
      )}
      {!!imports.data?.length && (
        <details>
          <summary className="cursor-pointer text-sm text-ink-300">
            Recent imports
          </summary>
          <ul className="mt-3 space-y-2 text-xs text-ink-500">
            {imports.data.map((r) => (
              <li key={r.id}>
                {r.source} · {timeAgo(r.created_at)} ·{" "}
                {r.error || `${r.summary?.days_touched || 0} days`}
              </li>
            ))}
          </ul>
        </details>
      )}
    </Section>
  );
}
function TokenSettings() {
  const tokens = useResource(() => api.listTokens());
  const a = useAction();
  const [name, setName] = useState("Personal assistant");
  const [write, setWrite] = useState(false);
  const [secret, setSecret] = useState("");
  const endpoint = `${window.location.origin}/mcp`;
  return (
    <Section title="API & MCP access" id="tokens">
      <p className="text-sm text-ink-400">
        Let your own AI read your health summary, reports, recipes and journal.
        These MCP tools use your stored data without calling another model.
      </p>
      <p className="break-all rounded-xl bg-ink-950 p-3 font-mono text-xs text-vital-400">
        {endpoint}
      </p>
      <form
        className="space-y-3"
        onSubmit={(e) => {
          e.preventDefault();
          void a.run(async () => {
            const result = await api.createToken({
              name,
              scopes: write ? ["read", "write"] : ["read"],
              expires_in_days: 90,
            });
            setSecret(result.secret);
            tokens.reload();
          }, "Token created. Save it now; it is only shown once.");
        }}
      >
        <Field label="Token name">
          <input
            className="field"
            required
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </Field>
        <label className="flex gap-2 text-sm text-ink-400">
          <input
            type="checkbox"
            checked={write}
            onChange={(e) => setWrite(e.target.checked)}
          />
          Allow changes and imports (read + write)
        </label>
        <button className="btn-primary" disabled={a.busy}>
          Create 90-day token
        </button>
      </form>
      {a.feedback}
      <ErrorText>{tokens.error}</ErrorText>
      {secret && (
        <div className="space-y-3">
          <code className="block break-all rounded-xl border border-vital-500/30 bg-ink-950 p-3 text-xs text-vital-400">
            {secret}
          </code>
          <p className="text-xs text-ink-400">
            Configure an HTTP MCP server with the URL above and the header
            Authorization: Bearer &lt;your token&gt;.
          </p>
          <button
            className="btn-ghost btn-sm"
            onClick={() =>
              void a.run(() => navigator.clipboard.writeText(secret), "Copied")
            }
          >
            Copy token
          </button>
          <button
            className="btn-ghost btn-sm ml-2"
            onClick={() => setSecret("")}
          >
            Hide token
          </button>
        </div>
      )}
      <ul className="space-y-2">
        {tokens.data?.map((t) => (
          <li
            className="flex items-center justify-between gap-3 border-t border-ink-800 pt-3 text-sm"
            key={t.id}
          >
            <div>
              <p>{t.name}</p>
              <p className="text-xs text-ink-500">
                {t.prefix}… · {t.scopes.join(", ")} · used{" "}
                {timeAgo(t.last_used_at)}
              </p>
            </div>
            <button
              className="btn-danger btn-sm"
              disabled={a.busy}
              onClick={() => {
                if (
                  window.confirm(
                    `Revoke ${t.name}? Connected clients will lose access.`,
                  )
                )
                  void a.run(async () => {
                    await api.revokeToken(t.id);
                    tokens.reload();
                  }, "Token revoked");
              }}
            >
              Revoke
            </button>
          </li>
        ))}
      </ul>
    </Section>
  );
}
function PasswordSettings() {
  const a = useAction();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  return (
    <Section title="Password">
      <form
        className="space-y-3"
        onSubmit={(e) => {
          e.preventDefault();
          void a.run(async () => {
            await api.changePassword(current, next);
            setCurrent("");
            setNext("");
          }, "Password updated");
        }}
      >
        <Field label="Current password">
          <input
            className="field"
            type="password"
            autoComplete="current-password"
            required
            value={current}
            onChange={(e) => setCurrent(e.target.value)}
          />
        </Field>
        <Field label="New password">
          <input
            className="field"
            type="password"
            minLength={8}
            autoComplete="new-password"
            required
            value={next}
            onChange={(e) => setNext(e.target.value)}
          />
        </Field>
        {a.feedback}
        <button className="btn-ghost" disabled={a.busy}>
          Update password
        </button>
      </form>
    </Section>
  );
}
export function Settings() {
  const { user, logout } = useAuth();
  return (
    <div className="mx-auto max-w-3xl">
      <PageHeader
        title="You & your connections"
        subtitle="Your profile, goals and data sources."
        action={
          <button className="btn-ghost" onClick={logout}>
            Sign out
          </button>
        }
      />
      <div className="mb-5 flex flex-wrap gap-2 md:hidden">
        {[
          ["blood", "Blood work"],
          ["photos", "Photos"],
          ["journal", "Journal"],
          ["coach", "Coach"],
        ].map(([path, label]) => (
          <Link key={path} className="chip" to={`/app/${path}`}>
            {label}
          </Link>
        ))}
      </div>
      <div className="space-y-5">
        <ProfileSettings />
        <GoalSettings />
        <HealthImports />
        {user?.hard75_eligible && <Hard75Settings />}
        <TokenSettings />
        <PasswordSettings />
      </div>
    </div>
  );
}
