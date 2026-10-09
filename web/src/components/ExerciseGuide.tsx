import { useEffect, useState } from "react";
import type { Exercise, ExerciseHistory } from "../lib/types";
import { api } from "../lib/api";
import { categoryLabels, formatSets, videoSearchURL } from "../lib/training";
import { prettyDate } from "../lib/format";
import { MuscleLegend, MuscleMap } from "./MuscleMap";
import { Sheet } from "./ui";

function useReducedMotion() {
  const [reduced, setReduced] = useState(false);
  useEffect(() => {
    const q = window.matchMedia?.("(prefers-reduced-motion: reduce)");
    if (!q) return;
    setReduced(q.matches);
    const on = (e: MediaQueryListEvent) => setReduced(e.matches);
    q.addEventListener?.("change", on);
    return () => q.removeEventListener?.("change", on);
  }, []);
  return reduced;
}

/**
 * The start and end positions, alternating like a slow animation. With
 * reduced motion, or when paused, both frames sit side by side.
 */
export function ExerciseDemo({ exercise }: { exercise: Exercise }) {
  const reduced = useReducedMotion();
  const [frame, setFrame] = useState(0);
  const [paused, setPaused] = useState(false);
  const animate = !reduced && !paused && exercise.images.length > 1;
  useEffect(() => {
    if (!animate) return;
    const t = setInterval(() => setFrame((f) => (f + 1) % exercise.images.length), 1300);
    return () => clearInterval(t);
  }, [animate, exercise.images.length]);
  if (!exercise.images.length)
    return (
      <div className="flex aspect-[3/2] items-center justify-center rounded-2xl border border-dashed border-ink-700 bg-ink-950 px-6 text-center text-sm text-ink-500">
        No photos for this one yet — use the steps below, or watch a demonstration.
      </div>
    );
  if (!animate)
    return (
      <div className="grid grid-cols-2 gap-2">
        {exercise.images.map((src, i) => (
          <figure key={src}>
            <img src={src} alt={`${exercise.name}, ${i === 0 ? "start" : "end"} position`} className="aspect-[3/2] w-full rounded-xl object-cover" loading="lazy" />
            <figcaption className="mt-1 text-center text-[11px] text-ink-500">{i === 0 ? "Start" : "End"}</figcaption>
          </figure>
        ))}
        {!reduced && (
          <button type="button" className="col-span-2 text-xs text-vital-500" onClick={() => setPaused(false)}>
            Play the movement
          </button>
        )}
      </div>
    );
  return (
    <button
      type="button"
      className="relative block w-full overflow-hidden rounded-2xl"
      onClick={() => setPaused(true)}
      aria-label={`${exercise.name} demonstration, showing the ${frame === 0 ? "start" : "end"} position. Pause to compare positions.`}
    >
      {exercise.images.map((src, i) => (
        <img
          key={src}
          src={src}
          alt=""
          className={`aspect-[3/2] w-full object-cover transition-opacity duration-500 ${i === 0 ? "" : "absolute inset-0"} ${i === frame ? "opacity-100" : "opacity-0"}`}
        />
      ))}
      <span className="absolute bottom-2 left-2 rounded-full bg-black/55 px-2.5 py-1 text-[11px] font-medium text-white">
        {frame === 0 ? "Start" : "End"} · tap to pause
      </span>
    </button>
  );
}

export function ExerciseGuide({ exercise, showHistory = true }: { exercise: Exercise; showHistory?: boolean }) {
  const [history, setHistory] = useState<ExerciseHistory[] | null>(null);
  useEffect(() => {
    if (!showHistory) return;
    let live = true;
    setHistory(null);
    api.exerciseHistory(exercise.slug, exercise.name, 5).then((h) => live && setHistory(h)).catch(() => live && setHistory([]));
    return () => {
      live = false;
    };
  }, [exercise.slug, exercise.name, showHistory]);
  const meta = [
    categoryLabels[exercise.category],
    exercise.equipment === "dumbbell" ? "Dumbbell" : "Bodyweight",
    exercise.load ? `weight = ${exercise.load}` : "",
    exercise.per_side ? "reps per side" : "",
  ].filter(Boolean);
  return (
    <div className="space-y-5">
      <p className="text-xs text-ink-500">{meta.join(" · ")}</p>
      <ExerciseDemo exercise={exercise} />
      <p className="text-sm leading-relaxed text-ink-300">{exercise.summary}</p>
      <div className="grid items-center gap-4 sm:grid-cols-[240px_1fr]">
        <MuscleMap primary={exercise.primary} secondary={exercise.secondary} className="mx-auto w-full max-w-[260px]" />
        <MuscleLegend primary={exercise.primary} secondary={exercise.secondary} />
      </div>
      <section>
        <h3 className="mb-2 text-sm font-semibold">How to do it</h3>
        <ol className="list-decimal space-y-1.5 pl-5 text-sm leading-relaxed text-ink-300">
          {exercise.steps.map((s) => (
            <li key={s}>{s}</li>
          ))}
        </ol>
      </section>
      <div className="grid gap-3 sm:grid-cols-2">
        <section className="rounded-xl bg-vital-500/5 p-3">
          <h3 className="mb-1.5 text-xs font-semibold uppercase tracking-wide text-vital-500">Cues</h3>
          <ul className="space-y-1 text-sm text-ink-300">
            {exercise.cues.map((c) => (
              <li key={c}>✓ {c}</li>
            ))}
          </ul>
        </section>
        <section className="rounded-xl bg-rose-500/5 p-3">
          <h3 className="mb-1.5 text-xs font-semibold uppercase tracking-wide text-rose-400">Avoid</h3>
          <ul className="space-y-1 text-sm text-ink-300">
            {exercise.mistakes.map((m) => (
              <li key={m}>✕ {m}</li>
            ))}
          </ul>
        </section>
      </div>
      <a href={videoSearchURL(exercise.name)} target="_blank" rel="noreferrer" className="btn-ghost inline-flex w-full justify-center">
        Watch demonstrations ↗
      </a>
      {showHistory && history && history.length > 0 && (
        <section>
          <h3 className="mb-2 text-sm font-semibold">Your recent sessions</h3>
          <ul className="divide-y divide-ink-800 rounded-xl border border-ink-800">
            {history.map((h) => (
              <li key={`${h.workout_id}-${h.date}`} className="flex justify-between gap-3 px-3 py-2 text-sm">
                <span className="shrink-0 text-ink-500">{prettyDate(h.date)}</span>
                <span className="text-right text-ink-300">{formatSets(h.sets) || "No sets recorded"}</span>
              </li>
            ))}
          </ul>
        </section>
      )}
      <p className="text-[11px] leading-relaxed text-ink-500">
        General guidance, not a substitute for your trainer's coaching. Stop if something hurts. Photos: Free Exercise DB (public domain).
      </p>
    </div>
  );
}

export function ExerciseGuideSheet({ exercise, onClose }: { exercise: Exercise | null; onClose: () => void }) {
  return (
    <Sheet open={!!exercise} onClose={onClose} title={exercise?.name || "Exercise"} wide>
      {exercise && <ExerciseGuide exercise={exercise} />}
    </Sheet>
  );
}
