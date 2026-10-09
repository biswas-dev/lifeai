import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { api, type WorkoutBody } from "../lib/api";
import type { Exercise, ExerciseHistory, StravaStatus, WeightUnit, Workout, WorkoutSet } from "../lib/types";
import { emptySet, formatSets, muscleLabels, preferredUnit, rememberUnit } from "../lib/training";
import { prettyDate, todayISO } from "../lib/format";
import { message } from "../lib/useResource";
import { ExercisePicker, ExerciseThumb, useExerciseCatalog } from "../components/ExercisePicker";
import { ExerciseGuideSheet } from "../components/ExerciseGuide";
import { ErrorText, Field, PageHeader, Spinner } from "../components/ui";
import { TrashIcon } from "../components/Icons";

interface DraftExercise {
  key: string;
  slug: string;
  name: string;
  notes: string;
  sets: WorkoutSet[];
}

interface Draft {
  date: string;
  start: string;
  minutes: number | "";
  activity: string;
  kind: string;
  notes: string;
  exercises: DraftExercise[];
}

const DRAFT_KEY = "lifeai_session_draft";
let nextKey = 0;
const newKey = () => `ex-${Date.now()}-${nextKey++}`;

function localTime(iso: string | null) {
  if (!iso) return "";
  const d = new Date(iso);
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

function fromWorkout(w: Workout): Draft {
  return {
    date: w.date,
    start: localTime(w.started_at),
    minutes: w.minutes,
    activity: w.activity,
    kind: w.kind,
    notes: w.notes,
    exercises: (w.exercises || []).map((e) => ({ key: newKey(), slug: e.slug, name: e.name, notes: e.notes, sets: e.sets.map((s) => ({ ...s })) })),
  };
}

function blankDraft(date: string): Draft {
  const now = new Date();
  return {
    date,
    start: date === todayISO() ? `${String(now.getHours()).padStart(2, "0")}:${String(now.getMinutes()).padStart(2, "0")}` : "",
    minutes: 60,
    activity: "Strength session",
    kind: "strength",
    notes: "",
    exercises: [],
  };
}

function readDraft(): Draft | null {
  try {
    const raw = localStorage.getItem(DRAFT_KEY);
    return raw ? (JSON.parse(raw) as Draft) : null;
  } catch {
    return null;
  }
}

function writeDraft(d: Draft | null) {
  try {
    if (d) localStorage.setItem(DRAFT_KEY, JSON.stringify(d));
    else localStorage.removeItem(DRAFT_KEY);
  } catch {
    // Private mode: the session simply is not kept across reloads.
  }
}

function toBody(d: Draft): WorkoutBody {
  return {
    date: d.date,
    kind: d.kind,
    activity: d.activity.trim(),
    minutes: Number(d.minutes) || 0,
    started_at: d.start ? new Date(`${d.date}T${d.start}`).toISOString() : null,
    notes: d.notes,
    exercises: d.exercises.map((e) => ({ slug: e.slug, name: e.name, notes: e.notes, sets: e.sets })),
  };
}

function NumberCell({ value, onChange, label, step = "1", suffix }: { value: number | null; onChange: (v: number | null) => void; label: string; step?: string; suffix?: string }) {
  return (
    <div className="relative min-w-0 flex-1">
      <input
        aria-label={suffix ? `${label} (${suffix})` : label}
        type="number"
        inputMode="decimal"
        min={0}
        step={step}
        className="field h-11 w-full px-2 pr-7 text-center tabular-nums"
        value={value ?? ""}
        placeholder="–"
        onChange={(e) => onChange(e.target.value === "" ? null : Number(e.target.value))}
      />
      {suffix && (
        <span aria-hidden="true" className="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2 text-[10px] text-ink-500">
          {suffix}
        </span>
      )}
    </div>
  );
}

function ExerciseCard({
  item,
  exercise,
  index,
  count,
  unit,
  onChange,
  onRemove,
  onMove,
  onInfo,
  workoutID,
}: {
  workoutID: number | null;
  item: DraftExercise;
  exercise?: Exercise;
  index: number;
  count: number;
  unit: WeightUnit;
  onChange: (next: DraftExercise) => void;
  onRemove: () => void;
  onMove: (delta: number) => void;
  onInfo: () => void;
}) {
  const [last, setLast] = useState<ExerciseHistory | null | undefined>(undefined);
  useEffect(() => {
    let live = true;
    api
      .exerciseHistory(item.slug, item.name, 3)
      .then((h) => live && setLast(h.find((x) => x.workout_id !== workoutID) || null))
      .catch(() => live && setLast(null));
    return () => {
      live = false;
    };
  }, [item.slug, item.name, workoutID]);
  const mode = exercise?.mode || "weight_reps";
  const showWeight = mode === "weight_reps" || !exercise;
  const showReps = mode !== "time";
  const showSeconds = mode === "time";
  const setAt = (i: number, patch: Partial<WorkoutSet>) => onChange({ ...item, sets: item.sets.map((s, j) => (j === i ? { ...s, ...patch } : s)) });
  const addSet = () => {
    const prev = item.sets[item.sets.length - 1];
    onChange({ ...item, sets: [...item.sets, prev ? { ...prev } : emptySet(exercise, unit)] });
  };
  return (
    <article className="card p-4">
      <div className="flex items-start gap-3">
        <button type="button" onClick={onInfo} aria-label={`How to do ${item.name}`} className="shrink-0">
          <ExerciseThumb exercise={exercise} size={52} />
        </button>
        <div className="min-w-0 flex-1">
          <div className="flex items-start justify-between gap-2">
            <h3 className="font-semibold leading-snug text-ink-100">
              <span className="mr-1.5 text-ink-500">{index + 1}.</span>
              {item.name}
            </h3>
            <div className="flex shrink-0 items-center">
              <button type="button" className="p-2 text-ink-500 disabled:opacity-30" disabled={index === 0} onClick={() => onMove(-1)} aria-label="Move up">
                ↑
              </button>
              <button type="button" className="p-2 text-ink-500 disabled:opacity-30" disabled={index === count - 1} onClick={() => onMove(1)} aria-label="Move down">
                ↓
              </button>
              <button type="button" className="p-2 text-ink-500 hover:text-rose-400" onClick={onRemove} aria-label={`Remove ${item.name}`}>
                <TrashIcon size={15} />
              </button>
            </div>
          </div>
          <p className="text-xs text-ink-500">
            {exercise ? exercise.primary.map((m) => muscleLabels[m]).join(", ") : "Custom exercise"}
            {exercise && (
              <>
                {" · "}
                <button type="button" className="font-medium text-vital-500" onClick={onInfo}>
                  Form guide
                </button>
              </>
            )}
          </p>
          {last && (
            <p className="mt-1 text-xs text-ink-400">
              Last time ({prettyDate(last.date)}): {formatSets(last.sets) || "no sets recorded"}
            </p>
          )}
        </div>
      </div>
      {item.sets.length > 0 && (
        <div className="mt-3 space-y-2">
          <div className="flex gap-2 px-1 text-[10px] font-medium uppercase tracking-wide text-ink-500">
            <span className="w-7">Set</span>
            {showWeight && <span className="flex-1 text-center">{exercise?.load === "one dumbbell" ? "Dumbbell" : exercise ? "Per dumbbell" : "Weight"}</span>}
            {showReps && <span className="flex-1 text-center">{exercise?.per_side ? "Reps / side" : "Reps"}</span>}
            {showSeconds && <span className="flex-1 text-center">Seconds</span>}
            <span className="w-9" />
          </div>
          {item.sets.map((s, i) => (
            <div key={i} className="flex items-center gap-2">
              <span className="w-7 text-center text-sm font-medium text-ink-500">{i + 1}</span>
              {showWeight && <NumberCell label={`Set ${i + 1} weight`} step="0.5" suffix={s.unit} value={s.weight} onChange={(v) => setAt(i, { weight: v })} />}
              {showReps && <NumberCell label={`Set ${i + 1} reps`} value={s.reps} onChange={(v) => setAt(i, { reps: v == null ? null : Math.round(v) })} />}
              {showSeconds && <NumberCell label={`Set ${i + 1} seconds`} value={s.seconds} onChange={(v) => setAt(i, { seconds: v == null ? null : Math.round(v) })} />}
              <button type="button" className="flex h-9 w-9 items-center justify-center text-ink-500 hover:text-rose-400" onClick={() => onChange({ ...item, sets: item.sets.filter((_, j) => j !== i) })} aria-label={`Remove set ${i + 1}`}>
                ×
              </button>
            </div>
          ))}
        </div>
      )}
      <div className="mt-3 flex gap-2">
        <button type="button" className="btn-ghost flex-1" onClick={addSet}>
          + Add set
        </button>
      </div>
      <input className="field mt-2 text-sm" placeholder="Notes: cues, variation, how it felt…" value={item.notes} onChange={(e) => onChange({ ...item, notes: e.target.value })} aria-label={`${item.name} notes`} />
    </article>
  );
}

export function TrainingSession() {
  const { id } = useParams();
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const workoutID = id && id !== "new" ? Number(id) : null;
  const { catalog, bySlug, error: catalogError } = useExerciseCatalog();
  const [draft, setDraft] = useState<Draft | null>(null);
  const [saved, setSaved] = useState<Workout | null>(null);
  const [dirty, setDirty] = useState(false);
  const [restored, setRestored] = useState(false);
  const [unit, setUnitState] = useState<WeightUnit>(preferredUnit());
  const [picker, setPicker] = useState(false);
  const [guide, setGuide] = useState<Exercise | null>(null);
  const [busy, setBusy] = useState<"" | "save" | "strava" | "repeat" | "delete">("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [strava, setStrava] = useState<StravaStatus | null>(null);
  const [recent, setRecent] = useState<Workout[]>([]);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const loaded = useRef(false);

  useEffect(() => {
    loaded.current = false;
    setError("");
    setNotice("");
    if (workoutID) {
      api
        .workout(workoutID)
        .then((w) => {
          setSaved(w);
          setDraft(fromWorkout(w));
          setDirty(false);
          loaded.current = true;
        })
        .catch((e) => setError(message(e)));
    } else {
      const kept = readDraft();
      const date = params.get("date") || todayISO();
      setRestored(!!kept && kept.exercises.length > 0);
      setDraft(kept && kept.exercises.length > 0 ? kept : blankDraft(date));
      setSaved(null);
      loaded.current = true;
      // Starting a fixed session takes its title, time and length.
      const slotKey = params.get("slot");
      if (slotKey && !(kept && kept.exercises.length > 0))
        api
          .trainingSchedule()
          .then(({ schedule }) => {
            const slot = schedule?.fixed.find((f) => f.key === slotKey);
            if (!slot) return;
            const [sh, sm] = slot.start.split(":").map(Number);
            const [eh, em] = slot.end.split(":").map(Number);
            const length = (eh * 60 + em - (sh * 60 + sm) + 1440) % 1440 || 60;
            setDraft((d) => (d && d.exercises.length === 0 ? { ...d, activity: slot.title, kind: slot.kind, start: slot.start, minutes: length } : d));
          })
          .catch(() => {});
    }
  }, [workoutID]);

  useEffect(() => {
    api.stravaStatus().then(setStrava).catch(() => setStrava(null));
    api
      .workouts({ kind: "strength", limit: 8 })
      .then(setRecent)
      .catch(() => setRecent([]));
  }, []);

  // Keep a new session across reloads until it is saved.
  useEffect(() => {
    if (!workoutID && draft && loaded.current) writeDraft(draft.exercises.length ? draft : null);
  }, [draft, workoutID]);

  const recentSlugs = useMemo(() => [...new Set(recent.flatMap((w) => (w.exercises || []).map((e) => e.slug)).filter(Boolean))], [recent]);
  const lastSession = recent.find((w) => (w.exercises || []).length > 0 && w.id !== workoutID);

  if (!draft)
    return error ? (
      <ErrorText>{error}</ErrorText>
    ) : (
      <div className="flex justify-center py-20">
        <Spinner />
      </div>
    );

  const update = (patch: Partial<Draft>) => {
    setDraft((d) => (d ? { ...d, ...patch } : d));
    setDirty(true);
  };
  const updateExercises = (fn: (list: DraftExercise[]) => DraftExercise[]) => {
    setDraft((d) => (d ? { ...d, exercises: fn(d.exercises) } : d));
    setDirty(true);
  };
  const setUnit = (u: WeightUnit) => {
    setUnitState(u);
    rememberUnit(u);
    updateExercises((list) => list.map((e) => ({ ...e, sets: e.sets.map((s) => ({ ...s, unit: u })) })));
  };

  function addExercise(pick: { exercise?: Exercise; name: string }) {
    setPicker(false);
    updateExercises((list) => [...list, { key: newKey(), slug: pick.exercise?.slug || "", name: pick.name, notes: "", sets: [emptySet(pick.exercise, unit)] }]);
  }

  function repeatLast() {
    if (!lastSession) return;
    updateExercises(() => fromWorkout(lastSession).exercises);
    if (lastSession.activity) update({ activity: lastSession.activity, minutes: lastSession.minutes });
    setNotice(`Copied ${lastSession.exercises?.length} exercises from ${prettyDate(lastSession.date)}. Adjust the weights as you go.`);
  }

  async function save() {
    if (!draft) return;
    setBusy("save");
    setError("");
    try {
      const w = await api.saveWorkout(workoutID, toBody(draft));
      setSaved(w);
      setDirty(false);
      setRestored(false);
      if (!workoutID) {
        writeDraft(null);
        navigate(`/app/training/${w.id}`, { replace: true });
      }
      setNotice("Session saved.");
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy("");
    }
  }

  async function pushToStrava() {
    if (!saved) return;
    setBusy("strava");
    setError("");
    try {
      const up = await api.pushWorkoutToStrava(saved.id);
      setSaved(up.workout);
      setNotice(up.created ? "Posted to Strava." : "Strava activity updated.");
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy("");
    }
  }

  async function remove() {
    if (!saved) return;
    if (!confirmDelete) {
      setConfirmDelete(true);
      return;
    }
    setBusy("delete");
    try {
      await api.deleteWorkout(saved.id);
      navigate("/app/training", { replace: true });
    } catch (e) {
      setError(message(e));
      setBusy("");
    }
  }

  const totalSets = draft.exercises.reduce((n, e) => n + e.sets.length, 0);
  return (
    <div className="mx-auto max-w-2xl">
      <Link to="/app/training" className="mb-3 inline-block text-xs text-ink-500">
        ← Training
      </Link>
      <PageHeader title={workoutID ? draft.activity || "Session" : "New session"} subtitle={`${prettyDate(draft.date, true)} · ${draft.exercises.length} exercises · ${totalSets} sets`} />
      <ErrorText>{error || catalogError}</ErrorText>
      {notice && <p className="mb-3 rounded-xl bg-vital-500/10 px-3 py-2 text-sm text-vital-500" role="status">{notice}</p>}
      {restored && !workoutID && (
        <p className="mb-3 flex items-center justify-between gap-3 rounded-xl border border-ink-800 bg-white px-3 py-2 text-sm text-ink-400">
          Restored your unsaved session.
          <button
            type="button"
            className="text-xs font-medium text-rose-400"
            onClick={() => {
              writeDraft(null);
              setRestored(false);
              setDraft(blankDraft(params.get("date") || todayISO()));
            }}
          >
            Discard
          </button>
        </p>
      )}

      <section className="card mb-4 grid grid-cols-2 gap-3 p-4 sm:grid-cols-4">
        <div className="col-span-2">
          <Field label="Title">
            <input className="field" value={draft.activity} onChange={(e) => update({ activity: e.target.value })} />
          </Field>
        </div>
        <Field label="Date">
          <input type="date" className="field" value={draft.date} max={todayISO()} onChange={(e) => update({ date: e.target.value })} />
        </Field>
        <Field label="Started">
          <input type="time" className="field" value={draft.start} onChange={(e) => update({ start: e.target.value })} />
        </Field>
        <Field label="Minutes">
          <input type="number" inputMode="numeric" min={1} className="field" value={draft.minutes} onChange={(e) => update({ minutes: e.target.value === "" ? "" : Number(e.target.value) })} />
        </Field>
        <Field label="Weights in">
          <div className="flex gap-1" role="group" aria-label="Weight unit">
            {(["lb", "kg"] as WeightUnit[]).map((u) => (
              <button key={u} type="button" className={`chip flex-1 justify-center ${unit === u ? "chip-active" : ""}`} aria-pressed={unit === u} onClick={() => setUnit(u)}>
                {u}
              </button>
            ))}
          </div>
        </Field>
        <div className="col-span-2">
          <Field label="Type">
            <select className="field" value={draft.kind} onChange={(e) => update({ kind: e.target.value })}>
              <option value="strength">Strength</option>
              <option value="hiit">HIIT / circuit</option>
              <option value="yoga">Mobility / yoga</option>
              <option value="other">Other</option>
            </select>
          </Field>
        </div>
      </section>

      {draft.exercises.length === 0 && (
        <div className="card mb-4 p-6 text-center">
          <p className="text-sm text-ink-400">Add exercises as you go, or start from your last session.</p>
          {lastSession && (
            <button type="button" className="btn-ghost mt-4" onClick={repeatLast}>
              Repeat {lastSession.activity || "last session"} ({prettyDate(lastSession.date)})
            </button>
          )}
        </div>
      )}

      <div className="space-y-3">
        {draft.exercises.map((item, i) => (
          <ExerciseCard
            key={item.key}
            item={item}
            workoutID={workoutID}
            exercise={bySlug.get(item.slug)}
            index={i}
            count={draft.exercises.length}
            unit={unit}
            onInfo={() => {
              const e = bySlug.get(item.slug);
              if (e) setGuide(e);
            }}
            onChange={(next) => updateExercises((list) => list.map((x) => (x.key === item.key ? next : x)))}
            onRemove={() => updateExercises((list) => list.filter((x) => x.key !== item.key))}
            onMove={(delta) =>
              updateExercises((list) => {
                const out = [...list];
                const [moved] = out.splice(i, 1);
                out.splice(i + delta, 0, moved);
                return out;
              })
            }
          />
        ))}
      </div>

      <button type="button" className="btn-ghost mt-3 w-full border-dashed py-4" onClick={() => setPicker(true)} disabled={!catalog}>
        + Add exercise
      </button>

      <Field label="Session notes">
        <textarea className="field mt-1 min-h-20" value={draft.notes} placeholder="Trainer feedback, energy, anything to remember" onChange={(e) => update({ notes: e.target.value })} />
      </Field>

      <div className="sticky bottom-20 z-20 mt-5 flex gap-2 rounded-2xl border border-ink-800 bg-white/95 p-3 shadow-lg backdrop-blur md:bottom-4">
        <button type="button" className="btn-primary flex-1" onClick={() => void save()} disabled={busy !== "" || !Number(draft.minutes) || (!dirty && !!saved)}>
          {busy === "save" ? "Saving…" : saved && !dirty ? "Saved" : "Save session"}
        </button>
        {saved && strava?.connected && strava.can_upload && (
          <button type="button" className="btn-ghost" onClick={() => void pushToStrava()} disabled={busy !== "" || dirty}>
            {busy === "strava" ? "Posting…" : saved.strava_id ? "Update Strava" : "Post to Strava"}
          </button>
        )}
      </div>
      {saved && (
        <div className="mt-3 flex flex-wrap items-center justify-between gap-2 text-xs text-ink-500">
          {saved.strava_id ? (
            <a className="font-medium text-vital-500" href={`https://www.strava.com/activities/${saved.strava_id}`} target="_blank" rel="noreferrer">
              View on Strava ↗
            </a>
          ) : strava && (!strava.connected || !strava.can_upload) ? (
            <Link to="/app/settings" className="font-medium text-vital-500">
              {strava.connected ? "Reconnect Strava to post sessions" : "Connect Strava to post sessions"} →
            </Link>
          ) : (
            <span />
          )}
          <button type="button" className="text-rose-400" onClick={() => void remove()} disabled={busy !== ""}>
            {confirmDelete ? "Tap again to delete" : "Delete session"}
          </button>
        </div>
      )}

      <ExercisePicker open={picker} onClose={() => setPicker(false)} onPick={addExercise} onInfo={setGuide} recent={recentSlugs} />
      <ExerciseGuideSheet exercise={guide} onClose={() => setGuide(null)} />
    </div>
  );
}
