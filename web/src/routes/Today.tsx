import { useCallback, useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../lib/api";
import type { Dashboard, Day } from "../lib/types";
import { prettyDate, weekday } from "../lib/format";
import { useAuth } from "../state/AuthContext";
import { useResource } from "../lib/useResource";
import { DayView } from "../components/DayView";
import { ErrorText, Spinner } from "../components/ui";
import { Sparkline } from "../components/Sparkline";
import { DropIcon, SparkIcon } from "../components/Icons";

export function Today() {
  const { user } = useAuth();
  const [dash, setDash] = useState<Dashboard | null>(null);
  const [error, setError] = useState("");
  const summary = useResource(() => api.healthSummary());
  const loadVersion = useRef(0);
  const load = useCallback(async (d?: Day) => {
    const version = ++loadVersion.current;
    if (d) setDash((current) => (current ? { ...current, today: d } : current));
    try {
      const next = await api.dashboard();
      if (version !== loadVersion.current) return;
      setDash(next);
      setError("");
    } catch (e) {
      if (version !== loadVersion.current) return;
      setError(e instanceof Error ? e.message : "Could not load your day");
    }
  }, []);
  useEffect(() => {
    void load();
  }, [load]);
  const pending = dash?.today.meals.some(
    (m) => m.estimate_status === "pending",
  );
  useEffect(() => {
    if (!pending) return;
    const t = setInterval(() => void load(), 4000);
    return () => clearInterval(t);
  }, [pending, load]);
  if (!dash)
    return error ? (
      <div>
        <ErrorText>{error}</ErrorText>
        <button className="btn-ghost mt-4" onClick={() => void load()}>
          Try again
        </button>
      </div>
    ) : (
      <div className="flex justify-center py-20">
        <Spinner />
      </div>
    );
  const st = dash.stats;
  const first = user?.name?.split(" ")[0];
  const hour = new Date().getHours();
  const greeting =
    hour < 12 ? "Good morning" : hour < 18 ? "Good afternoon" : "Good evening";
  const watched =
    summary.data?.blood.watch.filter((m) =>
      ["hba1c", "ldl", "alt"].includes(m.code),
    ) || [];
  return (
    <div>
      <div className="mb-5 flex flex-wrap items-end justify-between gap-3 md:mb-7 md:gap-4">
        <div>
          <p className="eyebrow mb-3 hidden md:block">
            Make a little room for you
          </p>
          <h1 className="text-2xl font-semibold tracking-[-0.045em] text-ink-100 md:text-4xl">
            {greeting}
            {first ? `, ${first}` : ""}.
          </h1>
          <p className="mt-1 text-xs text-ink-400 md:mt-2 md:text-sm">
            Make a note of what you do, whenever you do it.
          </p>
        </div>
        <Link to="/app/history" className="btn-ghost bg-white text-xs">
          {prettyDate(dash.today.date, true)} <span className="ml-2">↗</span>
        </Link>
      </div>
      <ErrorText>{error || summary.error}</ErrorText>
      <div className="grid items-start gap-6 xl:grid-cols-[minmax(0,1fr)_300px]">
        <div className="min-w-0">
          <div className="mb-3 flex items-center justify-between md:mb-4">
            <h2 className="text-sm font-semibold">Your week</h2>
            <span className="text-[11px] text-ink-500">This week</span>
          </div>
          <div className="mb-5 flex gap-2">
            {dash.week.map((d) => {
              const logged =
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
                  to={`/app/day/${d.date}`}
                  aria-label={prettyDate(d.date)}
                  className={`flex flex-1 flex-col items-center rounded-xl border py-2 text-[10px] md:py-3 ${d.date === dash.today.date ? "border-vital-500 bg-vital-500 text-white" : "border-ink-800 bg-white text-ink-500"}`}
                >
                  <span>{weekday(d.date)}</span>
                  <span className="mt-1 text-base font-medium md:mt-1.5">
                    {Number(d.date.slice(-2))}
                  </span>
                  <span
                    className={`mt-1 h-1 w-1 rounded-full md:mt-2 ${logged ? (d.date === dash.today.date ? "bg-white" : "bg-vital-500") : "bg-transparent"}`}
                  />
                </Link>
              );
            })}
          </div>
          <DayView day={dash.today} reload={load} />
        </div>
        <aside className="space-y-5">
          <section className="card overflow-hidden">
            <div className="flex items-center gap-2 border-b border-ink-800 p-5 text-sm font-semibold">
              <DropIcon size={17} />
              <h2>Beyond the everyday</h2>
            </div>
            <div className="p-5">
              <p className="text-xs text-ink-500">
                {summary.data?.blood.latest_report_date
                  ? `Blood work · ${prettyDate(summary.data.blood.latest_report_date)}`
                  : "Your blood work belongs here, too."}
              </p>
              {watched.length ? (
                <div className="mt-4 space-y-4">
                  {watched.map((m) => (
                    <div key={`${m.code}|${m.unit}`}>
                      <div className="flex items-center justify-between">
                        <span className="text-xs text-ink-300">{m.name}</span>
                        <span
                          className={`rounded-full px-2 py-0.5 text-[9px] ${m.flag === "high" || m.flag === "low" ? "bg-rose-500/10 text-rose-400" : "bg-ink-850 text-ink-500"}`}
                        >
                          {m.flag || "Recorded"}
                        </span>
                      </div>
                      <p className="mt-1.5 text-xl font-semibold">
                        {m.latest?.value}{" "}
                        <span className="text-[10px] font-normal text-ink-500">
                          {m.unit}
                        </span>
                      </p>
                    </div>
                  ))}
                </div>
              ) : (
                <p className="mt-3 text-sm leading-relaxed text-ink-400">
                  Add your first lab report to build a baseline and follow your
                  markers over time.
                </p>
              )}
              <Link
                className="mt-5 block border-t border-ink-800 pt-4 text-xs font-medium text-vital-500"
                to="/app/blood"
              >
                {watched.length ? "Review your reports" : "Add a blood report"}{" "}
                →
              </Link>
            </div>
          </section>
          <section className="card p-5">
            <div className="mb-4 flex justify-between">
              <h2 className="text-sm font-semibold">The longer view</h2>
              <span className="text-[10px] text-ink-500">30 days</span>
            </div>
            <Sparkline
              points={st.weight.map((p) => ({
                ...p,
                value: user?.weight_unit === "lb" ? p.value * 2.20462 : p.value,
              }))}
              unit={user?.weight_unit || "kg"}
              height={95}
            />
            <Link
              to="/app/trends"
              className="mt-4 block text-xs font-medium text-vital-500"
            >
              Explore your trends →
            </Link>
          </section>
          <section className="rounded-2xl border border-vital-500/15 bg-[#edf4ee] p-5">
            <SparkIcon size={19} />
            <h2 className="mt-3 text-lg font-semibold tracking-tight">
              A little perspective.
            </h2>
            <p className="mt-2 text-sm leading-relaxed text-ink-400">
              Bring your meals, movement and lab trends together. Ask for a
              daily note when you need one.
            </p>
            <Link
              to="/app/coach"
              className="mt-5 inline-flex text-xs font-medium text-vital-500"
            >
              Open health insights →
            </Link>
          </section>
          {dash.recent_recipes.length > 0 && (
            <section className="card p-5">
              <h2 className="mb-3 text-sm font-semibold">Worth making again</h2>
              {dash.recent_recipes.slice(0, 3).map((r) => (
                <Link
                  key={r.id}
                  to={`/app/recipes/${r.id}`}
                  className="block border-t border-ink-800 py-3"
                >
                  <p className="text-sm font-medium">{r.name}</p>
                  <p className="mt-1 text-xs text-ink-500">
                    {r.minutes} min · {Math.round(r.protein_g)} g protein
                  </p>
                </Link>
              ))}
            </section>
          )}
        </aside>
      </div>
    </div>
  );
}
