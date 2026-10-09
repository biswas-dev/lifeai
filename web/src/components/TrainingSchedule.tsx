import { useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../lib/api";
import type { ScheduleSlot, TrainingSchedule, WeekTraining } from "../lib/types";
import { clockLabel, WEEKDAY_NAMES } from "../lib/nutrition";
import { prettyDate } from "../lib/format";
import { message } from "../lib/useResource";
import { CheckIcon, TrashIcon } from "./Icons";
import { ErrorText, Field } from "./ui";

const ORDER = [1, 2, 3, 4, 5, 6, 0];

export function slotDays(days: number[]) {
  return ORDER.filter((d) => days.includes(d))
    .map((d) => WEEKDAY_NAMES[d].slice(0, 3))
    .join(" & ");
}

export function slotTime(s: { start: string; end: string }) {
  return `${clockLabel(s.start)}–${clockLabel(s.end)}`;
}

const statusStyle = {
  done: "text-vital-500",
  today: "text-ember-400 font-semibold",
  upcoming: "text-ink-400",
  missed: "text-rose-400",
};

/** The week's fixed sessions, and what freeform movement the goal still needs. */
export function WeekPlan({ week, limit }: { week: WeekTraining; limit?: number }) {
  const planned = week.planned || [];
  const shown = limit ? planned.filter((p) => p.status === "today" || p.status === "upcoming").slice(0, limit) : planned;
  if (!planned.length && !week.goal) return null;
  return (
    <div className="mt-4 space-y-2">
      {shown.map((p) => (
        <div key={`${p.date}-${p.key}`} className="flex items-center gap-3 text-sm">
          <span className={`flex w-5 shrink-0 justify-center ${statusStyle[p.status]}`} aria-hidden="true">
            {p.status === "done" ? <CheckIcon size={15} /> : p.status === "missed" ? "–" : "○"}
          </span>
          <span className="min-w-0 flex-1">
            <span className="block truncate">{p.title}</span>
            <span className={`block text-xs ${statusStyle[p.status]}`}>
              {p.status === "today" ? "Today" : prettyDate(p.date)} · {slotTime(p)}
              {p.status === "missed" ? " · missed" : ""}
            </span>
          </span>
          {p.status === "done" && p.workout_id ? (
            <Link to={`/app/training/${p.workout_id}`} className="text-xs font-medium text-vital-500">
              View
            </Link>
          ) : p.status === "today" || p.status === "missed" ? (
            <Link to={`/app/training/new?date=${p.date}&slot=${p.key}`} className="btn-ghost px-3 py-1.5 text-xs">
              {p.status === "today" ? "Start" : "Log it"}
            </Link>
          ) : null}
        </div>
      ))}
      {week.goal ? (
        <p className="rounded-xl bg-ink-950 px-3 py-2 text-xs leading-relaxed text-ink-400">
          {week.freeform_needed
            ? `Freeform: ${week.freeform_needed} more min this week${week.scheduled_left ? `, on top of ${week.scheduled_left} min of fixed sessions to come` : ""} — walks, rides, anything that fits.`
            : week.minutes >= week.goal
              ? "Weekly goal reached. Anything more is a bonus."
              : "Your remaining fixed sessions cover the rest of the goal."}
        </p>
      ) : null}
    </div>
  );
}

function blankSlot(): ScheduleSlot {
  return { key: "", title: "", kind: "strength", days: [], start: "06:30", end: "07:30" };
}

export function ScheduleEditor({ initial, onSaved, onCancel }: { initial: TrainingSchedule | null; onSaved: (s: TrainingSchedule) => void; onCancel: () => void }) {
  const [sc, setSc] = useState<TrainingSchedule>(() => structuredClone(initial) || { fixed: [{ ...blankSlot(), title: "Weight training", days: [1, 5] }], notes: "" });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const setSlot = (i: number, patch: Partial<ScheduleSlot>) => setSc((s) => ({ ...s, fixed: s.fixed.map((f, j) => (j === i ? { ...f, ...patch } : f)) }));
  async function save() {
    setBusy(true);
    setError("");
    try {
      const res = await api.saveTrainingSchedule(sc);
      onSaved(res.schedule);
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="space-y-3">
      {sc.fixed.map((f, i) => (
        <div key={i} className="space-y-3 rounded-xl border border-ink-800 p-3">
          <div className="flex gap-2">
            <input className="field flex-1" value={f.title} placeholder="Weight training with trainer" aria-label="Session title" onChange={(e) => setSlot(i, { title: e.target.value })} />
            <button type="button" className="p-2 text-ink-500 hover:text-rose-400" onClick={() => setSc({ ...sc, fixed: sc.fixed.filter((_, j) => j !== i) })} aria-label="Remove fixed session">
              <TrashIcon size={15} />
            </button>
          </div>
          <div className="flex flex-wrap gap-1" role="group" aria-label="Days">
            {ORDER.map((d) => {
              const on = f.days.includes(d);
              return (
                <button key={d} type="button" aria-pressed={on} className={`chip px-2.5 py-1 text-xs ${on ? "chip-active" : ""}`} onClick={() => setSlot(i, { days: on ? f.days.filter((x) => x !== d) : [...f.days, d] })}>
                  {WEEKDAY_NAMES[d].slice(0, 3)}
                </button>
              );
            })}
          </div>
          <div className="grid grid-cols-3 gap-2">
            <Field label="Start">
              <input type="time" className="field" value={f.start} onChange={(e) => setSlot(i, { start: e.target.value })} />
            </Field>
            <Field label="End">
              <input type="time" className="field" value={f.end} onChange={(e) => setSlot(i, { end: e.target.value })} />
            </Field>
            <Field label="Counts as">
              <select className="field" value={f.kind} onChange={(e) => setSlot(i, { kind: e.target.value })}>
                {["strength", "cardio", "walk", "run", "cycle", "swim", "yoga", "hiit", "sport", "other"].map((k) => (
                  <option key={k}>{k}</option>
                ))}
              </select>
            </Field>
          </div>
        </div>
      ))}
      <button type="button" className="text-xs font-medium text-vital-500" onClick={() => setSc({ ...sc, fixed: [...sc.fixed, blankSlot()] })}>
        + Add a fixed session
      </button>
      <Field label="Freeform" hint="What fills the rest of the week">
        <input className="field" value={sc.notes || ""} placeholder="Walks, rides, mobility — whenever they fit" onChange={(e) => setSc({ ...sc, notes: e.target.value })} />
      </Field>
      <ErrorText>{error}</ErrorText>
      <div className="flex gap-2">
        <button type="button" className="btn-primary flex-1" disabled={busy} onClick={() => void save()}>
          {busy ? "Saving…" : "Save schedule"}
        </button>
        <button type="button" className="btn-ghost" onClick={onCancel}>
          Cancel
        </button>
      </div>
    </div>
  );
}

export function ScheduleCard({ schedule, onSaved }: { schedule: TrainingSchedule | null; onSaved: (s: TrainingSchedule) => void }) {
  const [editing, setEditing] = useState(false);
  return (
    <section className="card p-4">
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-sm font-semibold">Weekly schedule</h2>
        {!editing && (
          <button type="button" className="text-xs font-medium text-vital-500" onClick={() => setEditing(true)}>
            {schedule ? "Edit" : "Set up"}
          </button>
        )}
      </div>
      {editing ? (
        <ScheduleEditor
          initial={schedule}
          onCancel={() => setEditing(false)}
          onSaved={(s) => {
            setEditing(false);
            onSaved(s);
          }}
        />
      ) : schedule && schedule.fixed.length ? (
        <ul className="space-y-2 text-sm">
          {schedule.fixed.map((f) => (
            <li key={f.key}>
              <span className="font-medium">{f.title}</span>
              <span className="block text-xs text-ink-500">
                {slotDays(f.days)} · {slotTime(f)}
              </span>
            </li>
          ))}
          <li className="text-xs text-ink-500">Freeform: {schedule.notes || "everything else, whenever it fits"}</li>
        </ul>
      ) : (
        <p className="text-sm text-ink-500">Fix the sessions that happen at set times — like training with your trainer — and fit everything else around them.</p>
      )}
    </section>
  );
}
