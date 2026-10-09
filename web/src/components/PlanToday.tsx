import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../lib/api";
import type { Day, NutritionPlan, PlanMeal } from "../lib/types";
import { MEAL_TIME_LABEL, loggedFor, optionFor, planKey, plannedTotals, weekdayOf, windowStatus } from "../lib/nutrition";
import { message } from "../lib/useResource";
import { CheckIcon } from "./Icons";
import { ErrorText } from "./ui";

function useNow(intervalMs = 60_000) {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const t = setInterval(() => setNow(new Date()), intervalMs);
    return () => clearInterval(t);
  }, [intervalMs]);
  return now;
}

export function EatingWindow({ plan }: { plan: NutritionPlan }) {
  const status = windowStatus(plan, useNow());
  if (!status) return null;
  const eating = status.state === "eating";
  return (
    <div className={`rounded-xl p-3 ${eating ? "bg-ember-500/10" : "bg-sky-500/10"}`} role="status">
      <div className="flex items-baseline justify-between gap-2">
        <p className={`text-sm font-semibold ${eating ? "text-ember-400" : "text-sky-400"}`}>{status.headline}</p>
      </div>
      <p className="mt-0.5 text-xs text-ink-400">{status.detail}</p>
      <div className="mt-2 h-1.5 overflow-hidden rounded-full bg-white/70">
        <div className={`h-full rounded-full ${eating ? "bg-ember-500" : "bg-sky-500"}`} style={{ width: `${Math.round(status.progress * 100)}%` }} />
      </div>
    </div>
  );
}

/** Today's planned meals, each ticked off by logging it. */
export function PlanToday({ day, plan, onChanged, showLink = true }: { day: Day; plan: NutritionPlan; onChanged: () => void; showLink?: boolean }) {
  const weekday = weekdayOf(day.date);
  const [choice, setChoice] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const planned = plannedTotals(plan, weekday);
  const optionOf = (m: PlanMeal) => m.options.find((o) => o.key === choice[m.key]) || optionFor(m, weekday);

  async function log(m: PlanMeal) {
    const o = optionOf(m);
    setBusy(m.key);
    setError("");
    try {
      await api.createMeal({
        date: day.date,
        name: o.title === m.title ? m.title : `${m.title}: ${o.title}`,
        slot: m.slot,
        kcal: o.kcal,
        protein_g: o.protein_g,
        carbs_g: o.carbs_g,
        fat_g: o.fat_g,
        notes: o.items.join("\n"),
        plan_key: planKey(m, o),
      });
      onChanged();
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy("");
    }
  }

  const loggedCount = plan.meals.filter((m) => loggedFor(day.meals, m)).length;
  return (
    <section className="card overflow-hidden">
      <div className="flex items-center justify-between border-b border-ink-800 p-4">
        <div>
          <h2 className="text-sm font-semibold">{day.is_today ? "Today's plan" : "Planned meals"}</h2>
          <p className="text-[11px] text-ink-500">
            {loggedCount}/{plan.meals.length} logged · {Math.round(day.totals.kcal)} / {Math.round(planned.kcal)} kcal · {Math.round(day.totals.protein_g)} / {Math.round(planned.protein_g)} g protein
          </p>
        </div>
        {showLink && (
          <Link to="/app/nutrition" className="text-xs font-medium text-vital-500">
            Plan →
          </Link>
        )}
      </div>
      <div className="space-y-3 p-4">
        {day.is_today && <EatingWindow plan={plan} />}
        <ErrorText>{error}</ErrorText>
        {plan.meals.map((m) => {
          const done = loggedFor(day.meals, m);
          const o = optionOf(m);
          return (
            <div key={m.key} className={`rounded-xl border p-3 ${done ? "border-vital-500/30 bg-vital-500/5" : "border-ink-800"}`}>
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <p className="text-[11px] font-medium uppercase tracking-wide text-ink-500">
                    {MEAL_TIME_LABEL(m)} · {m.title}
                  </p>
                  <p className="mt-0.5 font-medium text-ink-100">{done ? done.name : o.title}</p>
                </div>
                {done ? (
                  <span className="flex shrink-0 items-center gap-1 text-xs font-medium text-vital-500">
                    <CheckIcon size={14} /> Logged
                  </span>
                ) : (
                  <button type="button" className="btn-ghost shrink-0 px-3 text-xs" disabled={busy !== ""} onClick={() => void log(m)}>
                    {busy === m.key ? "Logging…" : "Log as eaten"}
                  </button>
                )}
              </div>
              {!done && (
                <>
                  {m.options.length > 1 && (
                    <select className="field mt-2 h-9 py-1 text-xs" value={o.key} onChange={(e) => setChoice((c) => ({ ...c, [m.key]: e.target.value }))} aria-label={`${m.title} option`}>
                      {m.options.map((opt) => (
                        <option key={opt.key} value={opt.key}>
                          {opt.title}
                        </option>
                      ))}
                    </select>
                  )}
                  <ul className="mt-2 space-y-0.5 text-xs leading-relaxed text-ink-400">
                    {o.items.map((it) => (
                      <li key={it}>· {it}</li>
                    ))}
                  </ul>
                  <p className="mt-1.5 text-[11px] text-ink-500">
                    ~{Math.round(o.kcal)} kcal · {Math.round(o.protein_g)} g protein · {Math.round(o.carbs_g)} g carbs · {Math.round(o.fat_g)} g fat
                  </p>
                </>
              )}
            </div>
          );
        })}
      </div>
    </section>
  );
}
