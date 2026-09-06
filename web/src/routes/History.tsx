import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../lib/api";
import type { DaySummary } from "../lib/types";
import { addDays, prettyDate, todayISO } from "../lib/format";
import { PageHeader, Spinner } from "../components/ui";

const MONTHS = [
  "January",
  "February",
  "March",
  "April",
  "May",
  "June",
  "July",
  "August",
  "September",
  "October",
  "November",
  "December",
];

/** A month grid: each day shows what was logged, tapping opens it. */
export function History() {
  const today = todayISO();
  const [month, setMonth] = useState(today.slice(0, 7));
  const [days, setDays] = useState<DaySummary[] | null>(null);

  useEffect(() => {
    const [y, m] = month.split("-").map(Number);
    const from = `${month}-01`;
    const lastDay = new Date(Date.UTC(y, m, 0)).getUTCDate();
    const to = `${month}-${String(lastDay).padStart(2, "0")}`;
    setDays(null);
    api
      .days(from, to)
      .then(setDays)
      .catch(() => setDays([]));
  }, [month]);

  const [y, m] = month.split("-").map(Number);
  const firstDow = new Date(Date.UTC(y, m - 1, 1)).getUTCDay();
  const prev = () => setMonth(addDays(`${month}-01`, -1).slice(0, 7));
  const next = () => setMonth(addDays(`${month}-01`, 32).slice(0, 7));

  const logged = (days || []).filter(
    (d) =>
      d.meals > 0 ||
      d.workout_minutes > 0 ||
      d.weight_kg != null ||
      d.journal > 0 ||
      d.photos > 0 ||
      (d.water_ml || 0) > 0 ||
      d.meditation_minutes > 0,
  );
  const kcalDays = (days || []).filter((d) => d.kcal > 0);
  const avgKcal = kcalDays.length
    ? Math.round(kcalDays.reduce((a, d) => a + d.kcal, 0) / kcalDays.length)
    : 0;
  const training = (days || []).reduce((a, d) => a + d.workout_minutes, 0);

  return (
    <div className="mx-auto max-w-3xl">
      <PageHeader
        title="Your calendar"
        stackOnMobile
        subtitle={`${logged.length} ${logged.length === 1 ? "day" : "days"} logged · avg ${avgKcal || "—"} kcal · ${training} min training`}
        action={
          <div className="flex items-center gap-2">
            <button
              className="btn-ghost min-h-11 min-w-11"
              onClick={prev}
              aria-label="Previous month"
            >
              ‹
            </button>
            <span className="min-w-[8rem] text-center text-sm text-ink-200">
              {MONTHS[m - 1]} {y}
            </span>
            <button
              className="btn-ghost min-h-11 min-w-11"
              onClick={next}
              aria-label="Next month"
              disabled={month >= today.slice(0, 7)}
            >
              ›
            </button>
          </div>
        }
      />
      {!days ? (
        <div className="flex justify-center py-20">
          <Spinner />
        </div>
      ) : (
        <div className="grid grid-cols-7 gap-1.5">
          {["S", "M", "T", "W", "T", "F", "S"].map((d, i) => (
            <div key={i} className="pb-1 text-center text-[11px] text-ink-600">
              {d}
            </div>
          ))}
          {Array.from({ length: firstDow }).map((_, i) => (
            <div key={`pad${i}`} />
          ))}
          {days.map((d) => {
            const future = d.date > today;
            const has =
              d.meals > 0 ||
              d.workout_minutes > 0 ||
              d.weight_kg != null ||
              d.journal > 0 ||
              d.photos > 0 ||
              (d.water_ml || 0) > 0 ||
              d.meditation_minutes > 0;
            return (
              <Link
                key={d.date}
                to={future ? "#" : `/app/day/${d.date}`}
                title={prettyDate(d.date)}
                aria-label={`${prettyDate(d.date)}${has ? ": " + [(d.water_ml || 0) > 0 && "water", d.meditation_minutes > 0 && "meditation", d.meals > 0 && "meals", d.workout_minutes > 0 && "exercise", d.weight_kg != null && "weight", d.journal > 0 && "journal", d.photos > 0 && "photos"].filter(Boolean).join(", ") : ""}`}
                aria-disabled={future}
                tabIndex={future ? -1 : undefined}
                className={`flex aspect-square flex-col items-center justify-center rounded-xl border text-xs transition ${
                  future
                    ? "border-ink-800/40 text-ink-700"
                    : d.date === today
                      ? "border-vital-500/60 bg-vital-500/10 text-vital-300"
                      : has
                        ? "border-ink-800 bg-ink-900 text-ink-200 hover:border-ink-600"
                        : "border-ink-800/60 text-ink-600 hover:border-ink-700"
                }`}
              >
                <span className="font-medium">{Number(d.date.slice(8))}</span>
                {has && (
                  <span className="mt-0.5 flex gap-0.5">
                    {(d.water_ml || 0) > 0 && (
                      <i className="h-1 w-1 rounded-full bg-sky-500" />
                    )}
                    {d.meditation_minutes > 0 && (
                      <i className="h-1 w-1 rounded-full bg-violet-400" />
                    )}
                    {d.meals > 0 && (
                      <i className="h-1 w-1 rounded-full bg-ember-400" />
                    )}
                    {d.workout_minutes > 0 && (
                      <i className="h-1 w-1 rounded-full bg-vital-400" />
                    )}
                    {d.weight_kg != null && (
                      <i className="h-1 w-1 rounded-full bg-sky-400" />
                    )}
                    {d.journal > 0 && (
                      <i className="h-1 w-1 rounded-full bg-ink-400" />
                    )}
                    {d.photos > 0 && (
                      <i className="h-1 w-1 rounded-full bg-rose-400" />
                    )}
                  </span>
                )}
              </Link>
            );
          })}
        </div>
      )}
      <div className="mt-4 flex flex-wrap gap-4 text-[11px] text-ink-500">
        <span>
          <i className="mr-1 inline-block h-1.5 w-1.5 rounded-full bg-sky-500" />
          water
        </span>
        <span>
          <i className="mr-1 inline-block h-1.5 w-1.5 rounded-full bg-violet-400" />
          meditation
        </span>
        <span>
          <i className="mr-1 inline-block h-1.5 w-1.5 rounded-full bg-ember-400" />
          meals
        </span>
        <span>
          <i className="mr-1 inline-block h-1.5 w-1.5 rounded-full bg-vital-400" />
          training
        </span>
        <span>
          <i className="mr-1 inline-block h-1.5 w-1.5 rounded-full bg-sky-400" />
          weight
        </span>
        <span>
          <i className="mr-1 inline-block h-1.5 w-1.5 rounded-full bg-ink-400" />
          journal
        </span>
        <span>
          <i className="mr-1 inline-block h-1.5 w-1.5 rounded-full bg-rose-400" />
          photos
        </span>
      </div>
    </div>
  );
}
