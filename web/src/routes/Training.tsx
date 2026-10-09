import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../lib/api";
import type { Exercise, ExerciseCategory, Muscle, WeekTraining, Workout } from "../lib/types";
import { addDays, minutes, prettyDate, todayISO, weekday } from "../lib/format";
import { useResource } from "../lib/useResource";
import { categoryLabels, exerciseSummary, muscleLabels, searchExercises, totalSets } from "../lib/training";
import { ExerciseFilters, ExerciseThumb, useExerciseCatalog } from "../components/ExercisePicker";
import { ExerciseGuideSheet } from "../components/ExerciseGuide";
import { MuscleMap } from "../components/MuscleMap";
import { ErrorText, PageHeader, Spinner } from "../components/ui";
import { DumbbellIcon } from "../components/Icons";
import { ScheduleCard, WeekPlan } from "../components/TrainingSchedule";

const kindLabel: Record<string, string> = {
  strength: "Strength",
  cardio: "Cardio",
  walk: "Walk",
  run: "Run",
  cycle: "Ride",
  swim: "Swim",
  yoga: "Mobility",
  hiit: "HIIT",
  sport: "Sport",
  other: "Exercise",
};

export function WeekProgress({ week, compact = false }: { week: WeekTraining; compact?: boolean }) {
  const goal = week.goal || 0;
  const pct = goal ? Math.min(100, Math.round((week.minutes / goal) * 100)) : 0;
  const left = Math.max(0, goal - week.minutes);
  const peak = Math.max(goal / 7, ...week.days.map((d) => d.minutes), 1);
  const today = todayISO();
  const daysLeft = week.days.filter((d) => d.date > today).length + (week.days.some((d) => d.date === today) ? 1 : 0);
  return (
    <div>
      <div className="flex items-baseline justify-between gap-3">
        <p className="text-2xl font-semibold tracking-tight">
          {week.minutes}
          <span className="text-sm font-normal text-ink-500">{goal ? ` / ${goal} min` : " min"}</span>
        </p>
        <p className="text-xs text-ink-500">
          {goal ? (left ? `${left} min to go${daysLeft > 0 && week.to >= today ? ` · ~${Math.ceil(left / Math.max(daysLeft, 1))} min/day` : ""}` : "Goal reached ✓") : `${week.sessions} sessions`}
        </p>
      </div>
      {goal > 0 && (
        <div className="mt-2 h-2 overflow-hidden rounded-full bg-ink-850" role="progressbar" aria-valuemin={0} aria-valuemax={goal} aria-valuenow={week.minutes} aria-label="Weekly exercise minutes">
          <div className="h-full rounded-full bg-vital-500 transition-all" style={{ width: `${pct}%` }} />
        </div>
      )}
      {!compact && (
        <div className="mt-4 flex items-end gap-1.5" aria-hidden="true">
          {week.days.map((d) => (
            <div key={d.date} className="flex flex-1 flex-col items-center gap-1">
              <div className="flex h-14 w-full items-end">
                <div
                  className={`w-full rounded-md ${d.minutes ? "bg-vital-500" : "bg-ink-850"} ${d.date === today ? "ring-2 ring-vital-500/30" : ""}`}
                  style={{ height: `${Math.max(4, Math.round((d.minutes / peak) * 56))}px` }}
                  title={`${d.minutes} min`}
                />
              </div>
              <span className={`text-[10px] ${d.date === today ? "font-semibold text-vital-500" : "text-ink-500"}`}>{weekday(d.date)}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function SessionRow({ w }: { w: Workout }) {
  const strength = (w.exercises || []).length > 0 || w.kind === "strength";
  const body = (
    <>
      <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-vital-500/10 text-vital-500">
        <DumbbellIcon size={18} />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate font-medium text-ink-100">{w.activity || kindLabel[w.kind] || w.kind}</span>
        <span className="block truncate text-xs text-ink-500">
          {prettyDate(w.date)} · {kindLabel[w.kind] || w.kind} · {minutes(w.minutes)}
          {w.exercises?.length ? ` · ${w.exercises.length} exercises, ${totalSets(w.exercises)} sets` : ""}
          {w.strava_id || (w.sources || []).includes("strava") ? " · Strava" : ""}
        </span>
        {w.exercises?.length ? <span className="mt-0.5 block truncate text-xs text-ink-400">{exerciseSummary(w.exercises)}</span> : null}
      </span>
      {strength && <span className="text-ink-500">›</span>}
    </>
  );
  return strength ? (
    <Link to={`/app/training/${w.id}`} className="flex items-center gap-3 py-3">
      {body}
    </Link>
  ) : (
    <div className="flex items-center gap-3 py-3">{body}</div>
  );
}

function Library({ onOpen }: { onOpen: (e: Exercise) => void }) {
  const { catalog, error } = useExerciseCatalog();
  const [q, setQ] = useState("");
  const [category, setCategory] = useState<ExerciseCategory | "">("");
  const [muscle, setMuscle] = useState<Muscle | "">("");
  const list = useMemo(() => (catalog ? searchExercises(catalog.exercises, q, category, muscle) : []), [catalog, q, category, muscle]);
  if (error) return <ErrorText>{error}</ErrorText>;
  if (!catalog)
    return (
      <div className="flex justify-center py-10">
        <Spinner />
      </div>
    );
  return (
    <div className="grid gap-5 lg:grid-cols-[260px_minmax(0,1fr)]">
      <div className="card p-4">
        <p className="mb-2 text-xs text-ink-500">Tap a muscle to see what trains it.</p>
        <MuscleMap primary={[]} selected={muscle} onSelect={(m) => setMuscle(muscle === m ? "" : m)} className="mx-auto w-full max-w-[260px]" />
        {muscle && (
          <button type="button" className="mt-2 w-full text-xs font-medium text-vital-500" onClick={() => setMuscle("")}>
            {muscleLabels[muscle]} · clear
          </button>
        )}
      </div>
      <div className="min-w-0">
        <ExerciseFilters catalog={catalog} q={q} setQ={setQ} category={category} setCategory={setCategory} muscle={muscle} setMuscle={setMuscle} />
        <ul className="mt-3 grid gap-2 sm:grid-cols-2">
          {list.map((e) => (
            <li key={e.slug}>
              <button type="button" onClick={() => onOpen(e)} className="card flex w-full items-center gap-3 p-2.5 text-left hover:border-vital-500/40">
                <ExerciseThumb exercise={e} size={60} />
                <span className="min-w-0">
                  <span className="block text-sm font-medium leading-snug text-ink-100">{e.name}</span>
                  <span className="block truncate text-xs text-ink-500">{categoryLabels[e.category]}</span>
                  <span className="block truncate text-xs text-vital-500">{e.primary.map((m) => muscleLabels[m]).join(", ")}</span>
                </span>
              </button>
            </li>
          ))}
        </ul>
        {list.length === 0 && <p className="py-8 text-center text-sm text-ink-500">Nothing matches — try another search.</p>}
      </div>
    </div>
  );
}

export function Training() {
  const [weekOf, setWeekOf] = useState(todayISO());
  const week = useResource(() => api.weekTraining(weekOf), [weekOf]);
  const sessions = useResource(() => api.workouts({ limit: 30 }));
  const schedule = useResource(() => api.trainingSchedule());
  const [guide, setGuide] = useState<Exercise | null>(null);
  const [tab, setTab] = useState<"log" | "library">("log");
  return (
    <div>
      <PageHeader
        title="Training"
        subtitle="Sessions, sets and how to do each movement well."
        action={
          <Link to={`/app/training/new?date=${todayISO()}`} className="btn-primary shrink-0">
            Start session
          </Link>
        }
      />
      <div className="mb-5 flex gap-2" role="tablist">
        {(
          [
            ["log", "Log"],
            ["library", "Exercise library"],
          ] as const
        ).map(([key, label]) => (
          <button key={key} role="tab" aria-selected={tab === key} type="button" className={`chip ${tab === key ? "chip-active" : ""}`} onClick={() => setTab(key)}>
            {label}
          </button>
        ))}
      </div>
      {tab === "library" ? (
        <Library onOpen={setGuide} />
      ) : (
        <div className="grid items-start gap-5 lg:grid-cols-[minmax(0,1fr)_340px]">
          <section className="card order-2 p-4 lg:order-1">
            <h2 className="mb-1 text-sm font-semibold">Recent sessions</h2>
            <ErrorText>{sessions.error}</ErrorText>
            {!sessions.data ? (
              <div className="flex justify-center py-8">
                <Spinner />
              </div>
            ) : sessions.data.length === 0 ? (
              <p className="py-6 text-sm text-ink-500">Nothing logged yet. Start a session when you train, or log a walk from Today.</p>
            ) : (
              <div className="divide-y divide-ink-800">
                {sessions.data.map((w) => (
                  <SessionRow key={w.id} w={w} />
                ))}
              </div>
            )}
          </section>
          <section className="card order-1 p-4 lg:order-2">
            <div className="mb-3 flex items-center justify-between">
              <h2 className="text-sm font-semibold">Movement this week</h2>
              <div className="flex items-center gap-1 text-xs text-ink-500">
                <button type="button" className="px-2 py-1" onClick={() => setWeekOf(addDays(week.data?.from || weekOf, -7))} aria-label="Previous week">
                  ‹
                </button>
                <span>{week.data ? `${prettyDate(week.data.from)} – ${prettyDate(week.data.to)}` : ""}</span>
                <button type="button" className="px-2 py-1 disabled:opacity-30" disabled={!week.data || week.data.to >= todayISO()} onClick={() => setWeekOf(addDays(week.data?.from || weekOf, 7))} aria-label="Next week">
                  ›
                </button>
              </div>
            </div>
            <ErrorText>{week.error}</ErrorText>
            {week.data ? (
              <>
                <WeekProgress week={week.data} />
                <WeekPlan week={week.data} />
              </>
            ) : (
              <Spinner />
            )}
            {week.data && !week.data.goal && (
              <Link to="/app/settings" className="mt-3 block text-xs font-medium text-vital-500">
                Set a weekly exercise goal →
              </Link>
            )}
          </section>
          {schedule.data && (
            <div className="order-3 lg:col-start-2">
              <ScheduleCard
                schedule={schedule.data.schedule}
                onSaved={(s) => {
                  schedule.setData({ schedule: s });
                  week.reload();
                }}
              />
            </div>
          )}
        </div>
      )}
      <ExerciseGuideSheet exercise={guide} onClose={() => setGuide(null)} />
    </div>
  );
}
