import { useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../lib/api";
import type { Day, Meal } from "../lib/types";
import { grams, kcal, minutes, weightDisplay } from "../lib/format";
import { useAuth } from "../state/AuthContext";
import { Bar, Empty, ErrorText } from "./ui";
import { AuthImage } from "./AuthImage";
import {
  CameraIcon,
  DumbbellIcon,
  LotusIcon,
  PenIcon,
  PlusIcon,
  ScaleIcon,
  TrashIcon,
} from "./Icons";
import {
  JournalSheet,
  MealSheet,
  MeditationSheet,
  MetricsSheet,
  WorkoutSheet,
} from "./sheets";
import { PhotoUpload } from "./PhotoUpload";

/** The day as a page: totals against goals, then everything logged. */
export function DayView({
  day,
  reload,
}: {
  day: Day;
  reload: (d?: Day) => void;
}) {
  const { user } = useAuth();
  const unit = user?.weight_unit || "kg";
  const [sheet, setSheet] = useState<
    "meal" | "metrics" | "workout" | "meditation" | "journal" | null
  >(null);
  const [error, setError] = useState("");
  const [editMeal, setEditMeal] = useState<Meal | null>(null);
  const g = day.goals;
  const t = day.totals;
  const m = day.metrics;

  const remaining = g.daily_kcal != null ? g.daily_kcal - t.kcal : null;

  async function del(
    kind: "meal" | "workout" | "meditation" | "journal" | "photo",
    id: number,
  ) {
    if (!confirm("Delete this entry?")) return;
    setError("");
    try {
      if (kind === "meal") await api.deleteMeal(id);
      if (kind === "workout") await api.deleteWorkout(id);
      if (kind === "meditation") await api.deleteMeditation(id);
      if (kind === "journal") await api.deleteJournal(id);
      if (kind === "photo") await api.deletePhoto(id);
      reload();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Could not delete entry");
    }
  }

  return (
    <div className="space-y-5">
      <ErrorText>{error}</ErrorText>
      <section className="card p-5">
        <h2 className="mb-5 text-sm font-semibold">Today’s nutrition</h2>
        <div className="mb-3 flex items-baseline justify-between">
          <div>
            <span className="text-3xl font-semibold tabular-nums text-ink-100">
              {kcal(t.kcal)}
            </span>
            <span className="text-sm text-ink-500">
              {" "}
              {g.daily_kcal ? `/ ${g.daily_kcal} kcal` : "kcal"}
            </span>
          </div>
          {remaining != null && (
            <span
              className={`text-sm tabular-nums ${remaining < 0 ? "text-rose-400" : "text-vital-400"}`}
            >
              {remaining < 0
                ? `${kcal(-remaining)} over`
                : `${kcal(remaining)} left`}
            </span>
          )}
        </div>
        <Bar value={t.kcal} target={g.daily_kcal} tone="ember" />
        <div className="mt-3 grid grid-cols-3 gap-3 text-xs">
          <Macro
            label="Protein"
            value={t.protein_g}
            target={g.protein_g}
            tone="vital"
          />
          <Macro
            label="Carbs"
            value={t.carbs_g}
            target={g.carbs_g}
            tone="sky"
          />
          <Macro label="Fat" value={t.fat_g} target={g.fat_g} tone="ember" />
        </div>
      </section>

      <div className="grid grid-cols-2 gap-2 sm:grid-cols-5">
        <button
          className="btn-primary"
          onClick={() => {
            setEditMeal(null);
            setSheet("meal");
          }}
        >
          <PlusIcon size={16} /> Meal
        </button>
        <button className="btn-ghost" onClick={() => setSheet("workout")}>
          <DumbbellIcon size={16} /> Workout
        </button>
        <button className="btn-ghost" onClick={() => setSheet("metrics")}>
          <ScaleIcon size={16} /> Body
        </button>
        <button className="btn-ghost" onClick={() => setSheet("meditation")}>
          <LotusIcon size={16} /> Meditate
        </button>
        <button className="btn-ghost" onClick={() => setSheet("journal")}>
          <PenIcon size={16} /> Journal
        </button>
      </div>

      <section>
        <h2 className="mb-2 text-sm font-medium uppercase tracking-wide text-ink-500">
          Body
        </h2>
        <div className="card grid grid-cols-3 gap-y-3 p-4 text-sm sm:grid-cols-6">
          <Cell label="Weight" value={weightDisplay(m.weight_kg, unit)} />
          <Cell
            label="Body fat"
            value={m.body_fat_pct != null ? `${m.body_fat_pct}%` : "—"}
          />
          <Cell
            label="Resting HR"
            value={m.resting_hr != null ? `${m.resting_hr}` : "—"}
          />
          <Cell
            label="Sleep"
            value={m.sleep_hours != null ? `${m.sleep_hours} h` : "—"}
            target={g.sleep_hours}
            raw={m.sleep_hours}
          />
          <Cell
            label="Steps"
            value={m.steps != null ? m.steps.toLocaleString() : "—"}
            target={g.steps}
            raw={m.steps}
          />
          <Cell
            label="Water"
            value={m.water_ml != null ? `${m.water_ml} ml` : "—"}
            target={g.water_ml}
            raw={m.water_ml}
          />
        </div>
        {(m.mood != null || m.energy != null || m.note) && (
          <div className="mt-2 px-1 text-sm text-ink-400">
            {m.mood != null && <span className="mr-3">Mood {m.mood}/5</span>}
            {m.energy != null && (
              <span className="mr-3">Energy {m.energy}/5</span>
            )}
            {m.note && <span className="text-ink-300">{m.note}</span>}
          </div>
        )}
      </section>

      <section>
        <div className="mb-2 flex items-center justify-between">
          <h2 className="text-sm font-medium uppercase tracking-wide text-ink-500">
            Meals · {t.meals}
          </h2>
          <PhotoUpload
            kind="food"
            date={day.date}
            autolog
            capture
            label="Snap & log"
            onUploaded={() => reload()}
            className="btn-ghost btn-sm"
          />
        </div>
        {day.meals.length === 0 ? (
          <Empty>No meals logged yet. Photograph a plate or add a meal.</Empty>
        ) : (
          <div className="space-y-2">
            {day.meals.map((meal) => (
              <div key={meal.id} className="card flex items-center gap-3 p-3">
                {meal.photo_url ? (
                  <AuthImage
                    src={`${meal.photo_url}?size=thumb`}
                    alt={meal.name}
                    className="h-14 w-14 shrink-0 rounded-xl object-cover"
                  />
                ) : (
                  <div className="flex h-14 w-14 shrink-0 items-center justify-center rounded-xl bg-ink-850 text-ink-600">
                    <CameraIcon size={18} />
                  </div>
                )}
                <div
                  role="button"
                  tabIndex={0}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") {
                      setEditMeal(meal);
                      setSheet("meal");
                    }
                  }}
                  className="min-w-0 flex-1 cursor-pointer text-left"
                  onClick={() => {
                    setEditMeal(meal);
                    setSheet("meal");
                  }}
                >
                  <div className="flex items-center gap-2">
                    <span className="truncate font-medium text-ink-100">
                      {meal.name ||
                        (meal.estimate_status === "pending"
                          ? "Estimating…"
                          : "Meal")}
                    </span>
                    <span className="chip py-0.5 text-[10px]">{meal.slot}</span>
                    {meal.source === "ai" && (
                      <span className="text-[10px] text-ink-500">AI</span>
                    )}
                    {meal.source === "75hard" && (
                      <span className="text-[10px] text-ink-500">75hard</span>
                    )}
                    {meal.recipe_id && (
                      <Link
                        to={`/app/recipes/${meal.recipe_id}`}
                        className="text-[10px] text-vital-400"
                        onClick={(e) => e.stopPropagation()}
                      >
                        recipe
                      </Link>
                    )}
                  </div>
                  <div className="text-xs text-ink-500">
                    {meal.estimate_status === "pending" ? (
                      "the numbers are on their way"
                    ) : meal.estimate_status === "failed" ? (
                      <span className="text-rose-400">
                        {meal.estimate_error || "estimate failed"} ·{" "}
                        <button
                          className="underline"
                          onClick={(e) => {
                            e.stopPropagation();
                            api.retryEstimate(meal.id).then(() => reload());
                          }}
                        >
                          retry
                        </button>
                      </span>
                    ) : (
                      `${kcal(meal.kcal)} kcal · P ${grams(meal.protein_g)} · C ${grams(meal.carbs_g)} · F ${grams(meal.fat_g)}`
                    )}
                  </div>
                </div>
                <button
                  className="p-2 text-ink-600 hover:text-rose-400"
                  onClick={() => del("meal", meal.id)}
                  aria-label="Delete"
                >
                  <TrashIcon size={16} />
                </button>
              </div>
            ))}
          </div>
        )}
      </section>

      <section>
        <h2 className="mb-2 text-sm font-medium uppercase tracking-wide text-ink-500">
          Training · {minutes(t.workout_minutes)}
        </h2>
        {day.workouts.length === 0 && day.meditations.length === 0 ? (
          <Empty>
            A walk, a workout, a moment of stillness. Log your first session.
          </Empty>
        ) : (
          <div className="space-y-2">
            {day.workouts.map((w) => (
              <div key={w.id} className="card flex items-center gap-3 p-3">
                <span className="flex h-10 w-10 items-center justify-center rounded-xl bg-vital-500/10 text-vital-400">
                  <DumbbellIcon size={18} />
                </span>
                <div className="min-w-0 flex-1">
                  <div className="truncate font-medium text-ink-100">
                    {w.activity || w.kind}
                  </div>
                  <div className="text-xs text-ink-500">
                    {w.kind} · {minutes(w.minutes)}
                    {w.kcal ? ` · ${kcal(w.kcal)} kcal` : ""}
                    {w.distance_km ? ` · ${w.distance_km.toFixed(1)} km` : ""}
                    {w.avg_hr ? ` · ${w.avg_hr} bpm` : ""}
                    {w.source !== "manual" ? ` · ${w.source}` : ""}
                  </div>
                </div>
                <button
                  className="p-2 text-ink-600 hover:text-rose-400"
                  onClick={() => del("workout", w.id)}
                  aria-label="Delete"
                >
                  <TrashIcon size={16} />
                </button>
              </div>
            ))}
            {day.meditations.map((s) => (
              <div key={s.id} className="card flex items-center gap-3 p-3">
                <span className="flex h-10 w-10 items-center justify-center rounded-xl bg-sky-500/10 text-sky-400">
                  <LotusIcon size={18} />
                </span>
                <div className="min-w-0 flex-1">
                  <div className="font-medium text-ink-100">
                    {minutes(s.minutes)} · {s.style.replace("_", " ")}
                  </div>
                  {s.notes && (
                    <div className="truncate text-xs text-ink-500">
                      {s.notes}
                    </div>
                  )}
                </div>
                <button
                  className="p-2 text-ink-600 hover:text-rose-400"
                  onClick={() => del("meditation", s.id)}
                  aria-label="Delete"
                >
                  <TrashIcon size={16} />
                </button>
              </div>
            ))}
          </div>
        )}
      </section>

      {day.journal.length > 0 && (
        <section>
          <h2 className="mb-2 text-sm font-medium uppercase tracking-wide text-ink-500">
            Journal
          </h2>
          <div className="space-y-2">
            {day.journal.map((e) => (
              <div key={e.id} className="card p-4">
                <div className="flex items-start justify-between gap-2">
                  <div>
                    {e.title && (
                      <div className="font-medium text-ink-100">{e.title}</div>
                    )}
                    <p className="whitespace-pre-wrap text-sm text-ink-300">
                      {e.body}
                    </p>
                  </div>
                  <button
                    className="p-1 text-ink-600 hover:text-rose-400"
                    onClick={() => del("journal", e.id)}
                    aria-label="Delete"
                  >
                    <TrashIcon size={16} />
                  </button>
                </div>
              </div>
            ))}
          </div>
        </section>
      )}

      <section>
        <div className="mb-2 flex items-center justify-between">
          <h2 className="text-sm font-medium uppercase tracking-wide text-ink-500">
            Photos · {day.photos.length}
          </h2>
          <PhotoUpload
            kind="progress"
            date={day.date}
            pose="front"
            capture
            label="Progress photo"
            onUploaded={() => reload()}
            className="btn-ghost btn-sm"
          />
        </div>
        {day.photos.length > 0 && (
          <div className="grid grid-cols-4 gap-2 sm:grid-cols-6">
            {day.photos.map((p) => (
              <div
                key={p.id}
                className="group relative aspect-square overflow-hidden rounded-xl bg-ink-850"
              >
                <AuthImage
                  src={p.thumb_url}
                  alt={p.caption || p.kind}
                  className="h-full w-full object-cover"
                />
                <span className="absolute bottom-1 left-1 rounded bg-black/60 px-1 text-[10px] text-ink-200">
                  {p.kind}
                  {p.pose ? ` · ${p.pose}` : ""}
                </span>
                <button
                  className="absolute right-1 top-1 hidden rounded bg-black/60 p-1 text-ink-300 group-hover:block"
                  onClick={() => del("photo", p.id)}
                  aria-label="Delete"
                >
                  <TrashIcon size={12} />
                </button>
              </div>
            ))}
          </div>
        )}
      </section>

      <MealSheet
        open={sheet === "meal"}
        onClose={() => setSheet(null)}
        date={day.date}
        meal={editMeal}
        onSaved={() => reload()}
      />
      <MetricsSheet
        open={sheet === "metrics"}
        onClose={() => setSheet(null)}
        date={day.date}
        metrics={day.metrics}
        unit={unit}
        onSaved={(d) => reload(d)}
      />
      <WorkoutSheet
        open={sheet === "workout"}
        onClose={() => setSheet(null)}
        date={day.date}
        onSaved={() => reload()}
      />
      <MeditationSheet
        open={sheet === "meditation"}
        onClose={() => setSheet(null)}
        date={day.date}
        onSaved={() => reload()}
      />
      <JournalSheet
        open={sheet === "journal"}
        onClose={() => setSheet(null)}
        date={day.date}
        onSaved={() => reload()}
      />
    </div>
  );
}

function Macro({
  label,
  value,
  target,
  tone,
}: {
  label: string;
  value: number;
  target: number | null;
  tone: "vital" | "sky" | "ember";
}) {
  return (
    <div>
      <div className="mb-1 flex justify-between text-ink-400">
        <span>{label}</span>
        <span className="tabular-nums text-ink-200">
          {grams(value)}
          {target ? ` / ${target}` : ""}
        </span>
      </div>
      <Bar value={value} target={target} tone={tone} />
    </div>
  );
}

function Cell({
  label,
  value,
  target,
  raw,
}: {
  label: string;
  value: string;
  target?: number | null;
  raw?: number | null;
}) {
  const hit = target != null && raw != null && raw >= target;
  return (
    <div>
      <div className="text-[11px] uppercase tracking-wide text-ink-500">
        {label}
      </div>
      <div
        className={`font-medium tabular-nums ${hit ? "text-vital-400" : "text-ink-100"}`}
      >
        {value}
      </div>
    </div>
  );
}
