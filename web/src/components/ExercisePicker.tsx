import { useEffect, useMemo, useState } from "react";
import type { Exercise, ExerciseCatalog, ExerciseCategory, Muscle } from "../lib/types";
import { api } from "../lib/api";
import { categoryLabels, muscleLabels, searchExercises } from "../lib/training";
import { ErrorText, Sheet, Spinner } from "./ui";

let cached: Promise<ExerciseCatalog> | null = null;

/** The catalog, fetched once per page load. */
export function useExerciseCatalog() {
  const [catalog, setCatalog] = useState<ExerciseCatalog | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    let live = true;
    cached ??= api.exercises();
    cached
      .then((c) => live && setCatalog(c))
      .catch((e) => {
        cached = null;
        if (live) setError(e instanceof Error ? e.message : "Could not load exercises");
      });
    return () => {
      live = false;
    };
  }, []);
  const bySlug = useMemo(() => new Map((catalog?.exercises || []).map((e) => [e.slug, e])), [catalog]);
  return { catalog, bySlug, error };
}

export function ExerciseThumb({ exercise, size = 56 }: { exercise?: Exercise; size?: number }) {
  if (!exercise?.images.length)
    return (
      <span
        aria-hidden="true"
        style={{ width: size, height: size }}
        className="flex shrink-0 items-center justify-center rounded-xl bg-vital-500/10 text-lg text-vital-500"
      >
        {exercise?.name[0] || "+"}
      </span>
    );
  return <img src={exercise.images[1] || exercise.images[0]} alt="" width={size} height={size} loading="lazy" style={{ width: size, height: size }} className="shrink-0 rounded-xl object-cover" />;
}

export function ExerciseFilters({
  catalog,
  q,
  setQ,
  category,
  setCategory,
  muscle,
  setMuscle,
}: {
  catalog: ExerciseCatalog;
  q: string;
  setQ: (v: string) => void;
  category: ExerciseCategory | "";
  setCategory: (v: ExerciseCategory | "") => void;
  muscle: Muscle | "";
  setMuscle: (v: Muscle | "") => void;
}) {
  return (
    <div className="space-y-3">
      <div className="flex gap-2">
        <input className="field flex-1" type="search" placeholder="Search: squat, row, curl…" value={q} onChange={(e) => setQ(e.target.value)} aria-label="Search exercises" />
        <select className="field w-36 shrink-0" value={muscle} onChange={(e) => setMuscle(e.target.value as Muscle | "")} aria-label="Filter by muscle">
          <option value="">Any muscle</option>
          {catalog.muscles.map((m) => (
            <option key={m} value={m}>
              {muscleLabels[m]}
            </option>
          ))}
        </select>
      </div>
      <div className="-mx-1 flex gap-2 overflow-x-auto px-1 pb-1" role="group" aria-label="Category">
        <button type="button" className={`chip shrink-0 ${category === "" ? "chip-active" : ""}`} aria-pressed={category === ""} onClick={() => setCategory("")}>
          All
        </button>
        {catalog.categories.map((c) => (
          <button type="button" key={c} className={`chip shrink-0 ${category === c ? "chip-active" : ""}`} aria-pressed={category === c} onClick={() => setCategory(c)}>
            {categoryLabels[c]}
          </button>
        ))}
      </div>
    </div>
  );
}

/** Choose an exercise to add to a session. */
export function ExercisePicker({
  open,
  onClose,
  onPick,
  onInfo,
  recent = [],
}: {
  open: boolean;
  onClose: () => void;
  onPick: (pick: { exercise?: Exercise; name: string }) => void;
  onInfo: (e: Exercise) => void;
  recent?: string[];
}) {
  const { catalog, error } = useExerciseCatalog();
  const [q, setQ] = useState("");
  const [category, setCategory] = useState<ExerciseCategory | "">("");
  const [muscle, setMuscle] = useState<Muscle | "">("");
  useEffect(() => {
    if (open) {
      setQ("");
      setCategory("");
      setMuscle("");
    }
  }, [open]);
  const list = useMemo(() => {
    if (!catalog) return [];
    const found = searchExercises(catalog.exercises, q, category, muscle);
    // Exercises from recent sessions first when browsing without a search.
    if (q || category || muscle) return found;
    const rank = (e: Exercise) => (recent.includes(e.slug) ? recent.indexOf(e.slug) : 999);
    return [...found].sort((a, b) => rank(a) - rank(b));
  }, [catalog, q, category, muscle, recent]);
  return (
    <Sheet open={open} onClose={onClose} title="Add an exercise" wide>
      <ErrorText>{error}</ErrorText>
      {!catalog ? (
        !error && (
          <div className="flex justify-center py-10">
            <Spinner />
          </div>
        )
      ) : (
        <>
          <ExerciseFilters catalog={catalog} q={q} setQ={setQ} category={category} setCategory={setCategory} muscle={muscle} setMuscle={setMuscle} />
          <ul className="mt-3 divide-y divide-ink-800">
            {list.map((e) => (
              <li key={e.slug} className="flex items-center gap-3 py-2">
                <button type="button" className="flex min-w-0 flex-1 items-center gap-3 text-left" onClick={() => onPick({ exercise: e, name: e.name })}>
                  <ExerciseThumb exercise={e} />
                  <span className="min-w-0">
                    <span className="block truncate text-sm font-medium text-ink-100">{e.name}</span>
                    <span className="block truncate text-xs text-ink-500">{e.primary.map((m) => muscleLabels[m]).join(", ")}</span>
                  </span>
                </button>
                <button type="button" className="btn-ghost shrink-0 px-3 text-xs" onClick={() => onInfo(e)} aria-label={`How to do ${e.name}`}>
                  Form
                </button>
              </li>
            ))}
          </ul>
          {q.trim() && (
            <button type="button" className="btn-ghost mt-3 w-full" onClick={() => onPick({ name: q.trim() })}>
              Add “{q.trim()}” as a custom exercise
            </button>
          )}
          {list.length === 0 && !q.trim() && <p className="py-6 text-center text-sm text-ink-500">No exercises match those filters.</p>}
        </>
      )}
    </Sheet>
  );
}
