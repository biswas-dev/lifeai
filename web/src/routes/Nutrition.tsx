import { useEffect, useState } from "react";
import { api } from "../lib/api";
import type { Day, Goals, NutritionPlan, PlanMeal, PlanOption, Slot } from "../lib/types";
import { MEAL_TIME_LABEL, WEEKDAY_NAMES, clockLabel, optionFor, plannedTotals, weekdayOf } from "../lib/nutrition";
import { prettyDate, todayISO } from "../lib/format";
import { message, useResource } from "../lib/useResource";
import { PlanToday } from "../components/PlanToday";
import { ErrorText, Field, PageHeader, Spinner } from "../components/ui";
import { TrashIcon } from "../components/Icons";

const ORDER = [1, 2, 3, 4, 5, 6, 0]; // Monday first
const slots: Slot[] = ["breakfast", "lunch", "dinner", "snack"];

const range = (lo?: number, hi?: number, unit = "") => (lo == null ? "—" : hi != null && hi !== lo ? `${lo}–${hi}${unit}` : `${lo}${unit}`);

function slug(s: string, taken: string[]) {
  const base = s.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "").slice(0, 30) || "item";
  let out = base;
  for (let i = 2; taken.includes(out); i++) out = `${base}-${i}`;
  return out;
}

function blankOption(taken: string[]): PlanOption {
  return { key: slug("option", taken), title: "", items: [], kcal: 0, protein_g: 0, carbs_g: 0, fat_g: 0, days: [] };
}

function blankMeal(taken: string[]): PlanMeal {
  return { key: slug("meal", taken), time: "12:00", title: "", slot: "lunch", options: [blankOption([])] };
}

function num(v: string) {
  return v === "" ? 0 : Number(v);
}

function PlanEditor({ initial, onSaved, onCancel }: { initial: NutritionPlan | null; onSaved: (p: NutritionPlan) => void; onCancel: () => void }) {
  const [plan, setPlan] = useState<NutritionPlan>(
    () => structuredClone(initial) || { name: "My plan", window_start: "", window_end: "", targets: {}, meals: [blankMeal([])], notes: [] },
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const setMeal = (i: number, patch: Partial<PlanMeal>) => setPlan((p) => ({ ...p, meals: p.meals.map((m, j) => (j === i ? { ...m, ...patch } : m)) }));
  const setOption = (i: number, k: number, patch: Partial<PlanOption>) =>
    setMeal(i, { options: plan.meals[i].options.map((o, j) => (j === k ? { ...o, ...patch } : o)) });
  const target = (key: keyof NutritionPlan["targets"], v: string) =>
    setPlan((p) => ({ ...p, targets: { ...p.targets, [key]: v === "" ? undefined : Number(v) } }));

  async function save() {
    setBusy(true);
    setError("");
    try {
      const clean: NutritionPlan = {
        ...plan,
        window_start: plan.window_start || undefined,
        window_end: plan.window_end || undefined,
        notes: (plan.notes || []).map((n) => n.trim()).filter(Boolean),
        meals: plan.meals.map((m) => ({
          ...m,
          title: m.title.trim() || m.key,
          end_time: m.end_time || undefined,
          options: m.options.map((o) => ({ ...o, title: o.title.trim() || m.title.trim() || o.key, items: o.items.map((x) => x.trim()).filter(Boolean) })),
        })),
      };
      const res = await api.saveNutritionPlan(clean);
      onSaved(res.plan);
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="space-y-4">
      <section className="card grid gap-3 p-4 sm:grid-cols-2">
        <Field label="Plan name">
          <input className="field" value={plan.name} onChange={(e) => setPlan({ ...plan, name: e.target.value })} />
        </Field>
        <Field label="From (who, when)">
          <input className="field" value={plan.source || ""} placeholder="Nutritionist, 9 Oct 2026" onChange={(e) => setPlan({ ...plan, source: e.target.value })} />
        </Field>
        <Field label="Eating window opens" hint="Leave both empty for no fasting window">
          <input type="time" className="field" value={plan.window_start || ""} onChange={(e) => setPlan({ ...plan, window_start: e.target.value })} />
        </Field>
        <Field label="Eating window closes">
          <input type="time" className="field" value={plan.window_end || ""} onChange={(e) => setPlan({ ...plan, window_end: e.target.value })} />
        </Field>
        <div className="grid grid-cols-2 gap-3 sm:col-span-2 sm:grid-cols-6">
          {(
            [
              ["kcal", "kcal from"],
              ["kcal_max", "kcal to"],
              ["protein_g", "Protein from (g)"],
              ["protein_g_max", "Protein to (g)"],
              ["carbs_g", "Carbs (g)"],
              ["fat_g", "Fat (g)"],
            ] as const
          ).map(([k, label]) => (
            <Field key={k} label={label}>
              <input type="number" inputMode="numeric" className="field" value={plan.targets[k] ?? ""} onChange={(e) => target(k, e.target.value)} />
            </Field>
          ))}
        </div>
      </section>

      {plan.meals.map((m, i) => (
        <section key={i} className="card space-y-3 p-4">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-semibold">Meal {i + 1}</h3>
            <button type="button" className="p-2 text-ink-500 hover:text-rose-400 disabled:opacity-30" disabled={plan.meals.length === 1} onClick={() => setPlan({ ...plan, meals: plan.meals.filter((_, j) => j !== i) })} aria-label={`Remove meal ${i + 1}`}>
              <TrashIcon size={15} />
            </button>
          </div>
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
            <div className="col-span-2">
              <Field label="Title">
                <input className="field" value={m.title} placeholder="Dinner" onChange={(e) => setMeal(i, { title: e.target.value })} />
              </Field>
            </div>
            <Field label="Time">
              <input type="time" className="field" value={m.time} onChange={(e) => setMeal(i, { time: e.target.value })} />
            </Field>
            <Field label="Until (optional)">
              <input type="time" className="field" value={m.end_time || ""} onChange={(e) => setMeal(i, { end_time: e.target.value })} />
            </Field>
            <Field label="Logged as">
              <select className="field" value={m.slot} onChange={(e) => setMeal(i, { slot: e.target.value as Slot })}>
                {slots.map((s) => (
                  <option key={s}>{s}</option>
                ))}
              </select>
            </Field>
          </div>
          {m.options.map((o, k) => (
            <div key={k} className="space-y-2 rounded-xl border border-ink-800 p-3">
              <div className="flex items-center gap-2">
                <input className="field flex-1" value={o.title} placeholder={m.options.length > 1 ? "Option, e.g. Salmon" : "What to eat"} onChange={(e) => setOption(i, k, { title: e.target.value })} aria-label="Option title" />
                {m.options.length > 1 && (
                  <button type="button" className="p-2 text-ink-500 hover:text-rose-400" onClick={() => setMeal(i, { options: m.options.filter((_, j) => j !== k) })} aria-label="Remove option">
                    <TrashIcon size={14} />
                  </button>
                )}
              </div>
              <textarea className="field min-h-20 text-sm" value={o.items.join("\n")} placeholder="One item per line, with portions" onChange={(e) => setOption(i, k, { items: e.target.value.split("\n") })} aria-label="Items" />
              <div className="grid grid-cols-4 gap-2">
                {(
                  [
                    ["kcal", "kcal"],
                    ["protein_g", "Protein g"],
                    ["carbs_g", "Carbs g"],
                    ["fat_g", "Fat g"],
                  ] as const
                ).map(([key, label]) => (
                  <label key={key} className="block">
                    <span className="text-[10px] text-ink-500">{label}</span>
                    <input type="number" inputMode="numeric" className="field h-9 px-2 text-sm" value={o[key] || ""} onChange={(e) => setOption(i, k, { [key]: num(e.target.value) })} />
                  </label>
                ))}
              </div>
              {m.options.length > 1 && (
                <div className="flex flex-wrap gap-1" role="group" aria-label="Planned weekdays">
                  {ORDER.map((d) => {
                    const on = o.days?.includes(d);
                    return (
                      <button key={d} type="button" aria-pressed={on} className={`chip px-2 py-1 text-[11px] ${on ? "chip-active" : ""}`} onClick={() => setOption(i, k, { days: on ? o.days?.filter((x) => x !== d) : [...(o.days || []), d].sort() })}>
                        {WEEKDAY_NAMES[d].slice(0, 3)}
                      </button>
                    );
                  })}
                </div>
              )}
            </div>
          ))}
          <button type="button" className="text-xs font-medium text-vital-500" onClick={() => setMeal(i, { options: [...m.options, blankOption(m.options.map((o) => o.key))] })}>
            + Add an option (e.g. a dinner rotation)
          </button>
        </section>
      ))}
      <button type="button" className="btn-ghost w-full" onClick={() => setPlan({ ...plan, meals: [...plan.meals, blankMeal(plan.meals.map((m) => m.key))] })}>
        + Add meal
      </button>
      <Field label="Notes and reminders" hint="One per line">
        <textarea className="field min-h-24 text-sm" value={(plan.notes || []).join("\n")} onChange={(e) => setPlan({ ...plan, notes: e.target.value.split("\n") })} />
      </Field>
      <ErrorText>{error}</ErrorText>
      <div className="flex gap-2">
        <button type="button" className="btn-primary flex-1" disabled={busy} onClick={() => void save()}>
          {busy ? "Saving…" : "Save plan"}
        </button>
        <button type="button" className="btn-ghost" onClick={onCancel}>
          Cancel
        </button>
      </div>
    </div>
  );
}

export function Nutrition() {
  const planRes = useResource(() => api.nutritionPlan());
  const [date] = useState(todayISO());
  const [day, setDay] = useState<Day | null>(null);
  const [editing, setEditing] = useState(false);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const loadDay = () => api.day(date).then(setDay).catch((e) => setError(message(e)));
  useEffect(() => {
    void loadDay();
  }, [date]);
  const plan = planRes.data?.plan || null;

  async function applyTargets() {
    if (!plan) return;
    try {
      const g: Goals = await api.goals();
      const t = plan.targets;
      const mid = (a?: number, b?: number) => (a == null ? null : Math.round(b != null ? (a + b) / 2 : a));
      await api.saveGoals({ ...g, daily_kcal: mid(t.kcal, t.kcal_max) ?? g.daily_kcal, protein_g: t.protein_g_max ?? t.protein_g ?? g.protein_g, carbs_g: t.carbs_g ?? g.carbs_g, fat_g: t.fat_g ?? g.fat_g });
      setNotice("Daily calorie and macro goals updated from the plan.");
      void loadDay();
    } catch (e) {
      setError(message(e));
    }
  }

  if (!planRes.data)
    return planRes.error ? (
      <ErrorText>{planRes.error}</ErrorText>
    ) : (
      <div className="flex justify-center py-20">
        <Spinner />
      </div>
    );

  if (editing || !plan)
    return (
      <div className="mx-auto max-w-3xl">
        <PageHeader title={plan ? "Edit nutrition plan" : "Set up your nutrition plan"} subtitle="Meals, portions and an optional eating window. Days can rotate options." />
        {!plan && !editing ? (
          <div className="card p-6 text-center">
            <p className="text-sm text-ink-400">Add the plan from your nutritionist to tick meals off each day and track your eating window.</p>
            <button type="button" className="btn-primary mt-4" onClick={() => setEditing(true)}>
              Create a plan
            </button>
          </div>
        ) : (
          <PlanEditor
            initial={plan}
            onCancel={() => setEditing(false)}
            onSaved={(p) => {
              planRes.setData({ plan: p });
              setEditing(false);
              setNotice("Plan saved.");
            }}
          />
        )}
      </div>
    );

  const weekday = weekdayOf(date);
  const totals = plannedTotals(plan, weekday);
  return (
    <div>
      <PageHeader
        title={plan.name}
        subtitle={[plan.source, plan.window_start && plan.window_end ? `Eat ${clockLabel(plan.window_start)}–${clockLabel(plan.window_end)}` : ""].filter(Boolean).join(" · ")}
        action={
          <button type="button" className="btn-ghost shrink-0" onClick={() => setEditing(true)}>
            Edit plan
          </button>
        }
      />
      <ErrorText>{error}</ErrorText>
      {notice && <p className="mb-3 rounded-xl bg-vital-500/10 px-3 py-2 text-sm text-vital-500" role="status">{notice}</p>}
      <div className="grid items-start gap-5 lg:grid-cols-[minmax(0,1fr)_380px]">
        <div className="space-y-5">
          {day ? <PlanToday day={day} plan={plan} onChanged={() => void loadDay()} showLink={false} /> : <Spinner />}
          <section className="card overflow-x-auto p-4">
            <h2 className="mb-3 text-sm font-semibold">The week</h2>
            <table className="w-full min-w-[520px] text-left text-xs">
              <thead>
                <tr className="text-ink-500">
                  <th className="py-1.5 pr-3 font-medium">Day</th>
                  {plan.meals.map((m) => (
                    <th key={m.key} className="py-1.5 pr-3 font-medium">
                      {m.title}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y divide-ink-800">
                {ORDER.map((d) => (
                  <tr key={d} className={d === weekday ? "bg-vital-500/5 font-medium" : ""}>
                    <td className="py-2 pr-3">{WEEKDAY_NAMES[d]}</td>
                    {plan.meals.map((m) => (
                      <td key={m.key} className="py-2 pr-3 text-ink-300">
                        {optionFor(m, d).title}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </section>
        </div>
        <aside className="space-y-5">
          <section className="card p-4">
            <h2 className="mb-3 text-sm font-semibold">Daily targets</h2>
            <dl className="grid grid-cols-2 gap-3 text-sm">
              <div>
                <dt className="text-xs text-ink-500">Calories</dt>
                <dd className="font-semibold">{range(plan.targets.kcal, plan.targets.kcal_max, " kcal")}</dd>
              </div>
              <div>
                <dt className="text-xs text-ink-500">Protein</dt>
                <dd className="font-semibold">{range(plan.targets.protein_g, plan.targets.protein_g_max, " g")}</dd>
              </div>
              <div>
                <dt className="text-xs text-ink-500">Planned today</dt>
                <dd>
                  {Math.round(totals.kcal)} kcal · {Math.round(totals.protein_g)} g protein
                </dd>
              </div>
              <div>
                <dt className="text-xs text-ink-500">Carbs / fat</dt>
                <dd>
                  {Math.round(totals.carbs_g)} g / {Math.round(totals.fat_g)} g
                </dd>
              </div>
            </dl>
            <button type="button" className="mt-4 text-xs font-medium text-vital-500" onClick={() => void applyTargets()}>
              Use these as my daily goals →
            </button>
          </section>
          <section className="card p-4">
            <h2 className="mb-2 text-sm font-semibold">Meals</h2>
            <ul className="space-y-2 text-sm">
              {plan.meals.map((m) => (
                <li key={m.key} className="flex justify-between gap-3">
                  <span>{m.title}</span>
                  <span className="text-ink-500">{MEAL_TIME_LABEL(m)}</span>
                </li>
              ))}
            </ul>
          </section>
          {plan.notes && plan.notes.length > 0 && (
            <section className="card p-4">
              <h2 className="mb-2 text-sm font-semibold">Remember</h2>
              <ul className="space-y-2 text-sm leading-relaxed text-ink-400">
                {plan.notes.map((n) => (
                  <li key={n}>· {n}</li>
                ))}
              </ul>
            </section>
          )}
          {plan.updated_at && <p className="text-[11px] text-ink-500">Updated {prettyDate(plan.updated_at.slice(0, 10), true)}</p>}
        </aside>
      </div>
    </div>
  );
}
