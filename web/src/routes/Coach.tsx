import { useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../lib/api";
import type { Plan } from "../lib/types";
import { message, useResource } from "../lib/useResource";
import { ErrorText, PageHeader, Spinner } from "../components/ui";

export function Coach() {
  const summary = useResource(() => api.healthSummary());
  const status = useResource(() => api.aiStatus());
  const [note, setNote] = useState("");
  const [plan, setPlan] = useState<Plan | null>(null);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  return (
    <div className="mx-auto max-w-3xl">
      <PageHeader
        title="Your health, in context"
        subtitle="Start with your record. Choose when to ask AI for help."
      />
      <ErrorText>{summary.error || status.error || error}</ErrorText>
      <section className="card p-5">
        <h2 className="font-semibold text-ink-100">What your record shows</h2>
        <p className="mt-1 text-xs text-ink-500">
          Computed directly from your data. No model call.
        </p>
        {!summary.data ? (
          !summary.error && <Spinner className="mt-4" />
        ) : (
          <ul className="mt-4 space-y-3 text-sm text-ink-300">
            {summary.data.signals.map((signal, i) => (
              <li key={i}>{signal}</li>
            ))}
          </ul>
        )}
        <Link
          className="mt-4 inline-block text-sm text-vital-400"
          to="/app/blood"
        >
          Review blood reports →
        </Link>
      </section>
      <section className="card mt-5 p-5">
        <h2 className="font-semibold text-ink-100">Ask your coach</h2>
        <p className="mt-2 text-sm text-ink-400">
          Generate a note or a weekly food and activity plan from your logs and
          goals. Review lab findings and health targets with your clinician.
        </p>
        <p className="mt-2 text-xs text-ink-500">
          {status.data?.enabled
            ? `${status.data.remaining} AI requests remaining today. Recent results are cached.`
            : "AI is unavailable until a model provider is configured. Your record and MCP analysis remain available."}
        </p>
        <div className="mt-4 flex flex-wrap gap-3">
          <button
            className="btn-primary"
            disabled={!!busy || !status.data?.enabled}
            onClick={async () => {
              setBusy("note");
              setError("");
              try {
                setNote((await api.coachNote()).note.note);
                status.reload();
              } catch (e) {
                setError(message(e));
              } finally {
                setBusy("");
              }
            }}
          >
            {busy === "note" ? "Reading your record…" : "Get a daily note"}
          </button>
          <button
            className="btn-ghost"
            disabled={!!busy || !status.data?.enabled}
            onClick={async () => {
              setBusy("plan");
              setError("");
              try {
                setPlan((await api.buildPlan()).plan);
                status.reload();
              } catch (e) {
                setError(message(e));
              } finally {
                setBusy("");
              }
            }}
          >
            {busy === "plan" ? "Building your plan…" : "Build a weekly plan"}
          </button>
        </div>
        {note && (
          <p className="mt-5 whitespace-pre-wrap text-sm leading-relaxed text-ink-200">
            {note}
          </p>
        )}
      </section>
      {plan && (
        <section className="mt-5 space-y-3">
          <h2 className="text-lg font-semibold">{plan.focus}</h2>
          <p className="text-sm text-ink-400">{plan.summary}</p>
          {plan.days.map((d) => (
            <div key={d.day} className="card p-5">
              <h3 className="font-medium text-vital-400">{d.day}</h3>
              <p className="mt-2 text-sm text-ink-200">{d.training}</p>
              <p className="mt-2 text-sm text-ink-300">{d.nutrition}</p>
              <p className="mt-2 text-xs text-ink-500">{d.note}</p>
            </div>
          ))}
          <ul className="space-y-2 text-sm text-ink-400">
            {plan.tips.map((t) => (
              <li key={t}>• {t}</li>
            ))}
          </ul>
        </section>
      )}
      <div className="mt-5 text-sm text-ink-400">
        Prefer your own AI?{" "}
        <Link className="text-vital-400" to="/app/settings#tokens">
          Create a read-only MCP token
        </Link>{" "}
        to analyze your record without additional model calls in lifeai.
      </div>
    </div>
  );
}
