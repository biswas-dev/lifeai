import type {
  Exercise,
  ExerciseCategory,
  Muscle,
  WeightUnit,
  WorkoutExercise,
  WorkoutSet,
} from "./types";

export const categoryLabels: Record<ExerciseCategory, string> = {
  warmup: "Warm-up & mobility",
  legs: "Legs & hips",
  push: "Chest & shoulders",
  pull: "Back",
  arms: "Arms",
  core: "Core",
};

export const muscleLabels: Record<Muscle, string> = {
  chest: "Chest",
  "front-delts": "Front shoulders",
  "side-delts": "Side shoulders",
  "rear-delts": "Rear shoulders",
  biceps: "Biceps",
  triceps: "Triceps",
  forearms: "Forearms",
  abs: "Abs",
  obliques: "Obliques",
  lats: "Lats",
  "upper-back": "Upper back",
  traps: "Traps",
  "lower-back": "Lower back",
  glutes: "Glutes",
  "hip-flexors": "Hip flexors",
  quads: "Quads",
  adductors: "Inner thighs",
  hamstrings: "Hamstrings",
  calves: "Calves",
};

/** A search for demonstration videos; no particular video is endorsed. */
export function videoSearchURL(name: string) {
  return `https://www.youtube.com/results?search_query=${encodeURIComponent(`how to ${name} proper form`)}`;
}

export function emptySet(ex: Exercise | undefined, unit: WeightUnit): WorkoutSet {
  return {
    reps: ex?.mode === "time" ? null : 10,
    weight: null,
    unit,
    seconds: ex?.mode === "time" ? 30 : null,
  };
}

function trimNumber(n: number) {
  return Number.isInteger(n) ? String(n) : n.toFixed(1).replace(/\.0$/, "");
}

export function formatSet(s: WorkoutSet) {
  const parts: string[] = [];
  if (s.weight) parts.push(`${trimNumber(s.weight)} ${s.unit}`);
  if (s.reps != null) parts.push(s.weight ? `× ${s.reps}` : `${s.reps} reps`);
  if (s.seconds != null) parts.push(`${s.seconds} s`);
  return parts.join(" ");
}

/** "16 lb × 10, 2× 20 lb × 10" — consecutive identical sets folded. */
export function formatSets(sets: WorkoutSet[]) {
  const out: string[] = [];
  for (let i = 0; i < sets.length; ) {
    let j = i + 1;
    const label = formatSet(sets[i]);
    while (j < sets.length && formatSet(sets[j]) === label) j++;
    if (label) out.push(j - i > 1 ? `${j - i}× ${label}` : label);
    i = j;
  }
  return out.join(", ");
}

/** The heaviest set, for progress at a glance. */
export function topSet(sets: WorkoutSet[]) {
  return sets.reduce<WorkoutSet | null>(
    (best, s) =>
      s.weight != null && (best?.weight == null || s.weight > best.weight)
        ? s
        : best,
    null,
  );
}

export function exerciseSummary(list: WorkoutExercise[] = []) {
  return list.map((e) => e.name).join(" · ");
}

export function totalSets(list: WorkoutExercise[] = []) {
  return list.reduce((n, e) => n + e.sets.length, 0);
}

export function searchExercises(
  list: Exercise[],
  q: string,
  category: ExerciseCategory | "",
  muscle: Muscle | "",
) {
  const needle = q.trim().toLowerCase();
  return list.filter(
    (e) =>
      (!category || e.category === category) &&
      (!muscle || e.primary.includes(muscle) || e.secondary.includes(muscle)) &&
      (!needle ||
        e.name.toLowerCase().includes(needle) ||
        e.aliases.some((a) => a.toLowerCase().includes(needle))),
  );
}

const UNIT_KEY = "lifeai_training_unit";

export function preferredUnit(): WeightUnit {
  try {
    return localStorage.getItem(UNIT_KEY) === "kg" ? "kg" : "lb";
  } catch {
    return "lb";
  }
}

export function rememberUnit(unit: WeightUnit) {
  try {
    localStorage.setItem(UNIT_KEY, unit);
  } catch {
    // Private mode: the default is fine.
  }
}
