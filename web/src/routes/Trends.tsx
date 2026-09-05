import { useState } from "react";
import { api } from "../lib/api";
import { useResource } from "../lib/useResource";
import { useAuth } from "../state/AuthContext";
import { ErrorText, PageHeader, Spinner, Stat } from "../components/ui";
import { Sparkline } from "../components/Sparkline";
import type { Point } from "../lib/types";

export function Trends() {
  const [days, setDays] = useState(90);
  const { user } = useAuth();
  const result = useResource(() => api.stats(days), [days]);
  const s = result.data;
  const charts: [string, Point[], string][] = s
    ? [
        [
          "Weight",
          s.weight.map((p) => ({
            ...p,
            value: user?.weight_unit === "lb" ? p.value * 2.20462 : p.value,
          })),
          user?.weight_unit || "kg",
        ],
        ["Resting heart rate", s.resting_hr, "bpm"],
        ["Sleep", s.sleep, "hours"],
        ["Steps", s.steps, "steps"],
        ["Calories logged", s.kcal, "kcal"],
        ["Protein logged", s.protein, "g"],
        ["Training", s.training, "min"],
        ["Body fat", s.body_fat, "%"],
        ["Mood", s.mood, "/ 5"],
      ]
    : [];
  return (
    <div className="mx-auto max-w-3xl">
      <PageHeader
        title="The bigger picture"
        subtitle="Look for patterns across the weeks."
        action={
          <select
            className="field w-auto"
            aria-label="Time period"
            value={days}
            onChange={(e) => setDays(Number(e.target.value))}
          >
            {[7, 30, 90, 180, 365].map((d) => (
              <option key={d} value={d}>
                {d} days
              </option>
            ))}
          </select>
        }
      />
      <ErrorText>{result.error}</ErrorText>
      {!s ? (
        !result.error && <Spinner />
      ) : (
        <>
          <div className="mb-5 grid grid-cols-2 gap-3 sm:grid-cols-4">
            <Stat
              label="Days logged"
              value={`${s.days_logged}/${s.days_in_window}`}
            />
            <Stat label="Training" value={s.workout_minutes} sub="minutes" />
            <Stat
              label="Avg sleep"
              value={s.avg_sleep ? s.avg_sleep.toFixed(1) : "—"}
              sub="hours on recorded days"
            />
            <Stat label="Streak" value={s.streak} sub="days" />
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            {charts.map(([label, points, unit]) => (
              <section className="card p-5" key={label}>
                <h2 className="mb-4 text-sm font-medium text-ink-300">
                  {label}
                </h2>
                <Sparkline points={points} unit={unit} height={100} />
                <div className="mt-2 flex justify-between text-[10px] text-ink-500">
                  <span>{points[0]?.date}</span>
                  <span>{points[points.length - 1]?.date}</span>
                </div>
              </section>
            ))}
          </div>
          <p className="mt-5 text-xs text-ink-500">
            Averages use recorded days. Missing entries are unknown; food photos
            produce estimates that you can correct.
          </p>
        </>
      )}
    </div>
  );
}
