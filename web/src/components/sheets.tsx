import { useEffect, useState } from "react";
import { api } from "../lib/api";
import type {
  Day,
  FoodEstimate,
  Meal,
  Metrics,
  Photo,
  Slot,
} from "../lib/types";
import { useAuth } from "../state/AuthContext";
import { waterConfig } from "../lib/water";
import { toKg, fromKg } from "../lib/format";
import { ErrorText, Field, NumberInput, Sheet } from "./ui";
import { PhotoUpload } from "./PhotoUpload";
import { AuthImage } from "./AuthImage";

const slots: Slot[] = ["breakfast", "lunch", "dinner", "snack"];

/** Log or edit a meal: by hand, from a photo estimate, or itemised. */
export function MealSheet({
  open,
  onClose,
  date,
  meal,
  onSaved,
}: {
  open: boolean;
  onClose: () => void;
  date: string;
  meal?: Meal | null;
  onSaved: (d?: Day) => void;
}) {
  const [name, setName] = useState("");
  const [slot, setSlot] = useState<Slot>("lunch");
  const [kcal, setKcal] = useState<number | "">("");
  const [protein, setProtein] = useState<number | "">("");
  const [carbs, setCarbs] = useState<number | "">("");
  const [fat, setFat] = useState<number | "">("");
  const [notes, setNotes] = useState("");
  const [photo, setPhoto] = useState<Photo | null>(null);
  const [estimate, setEstimate] = useState<FoodEstimate | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open) return;
    setName(meal?.name || "");
    setSlot(meal?.slot || guessSlot());
    setKcal(meal ? Math.round(meal.kcal) : "");
    setProtein(meal ? Math.round(meal.protein_g) : "");
    setCarbs(meal ? Math.round(meal.carbs_g) : "");
    setFat(meal ? Math.round(meal.fat_g) : "");
    setNotes(meal?.notes || "");
    setPhoto(null);
    setEstimate(null);
    setError("");
  }, [open, meal]);

  async function analyze(p: Photo) {
    setPhoto(p);
    setBusy(true);
    setError("");
    try {
      const res = await api.analyzeFood(p.id, name);
      setEstimate(res.estimate);
      if (!name) setName(res.estimate.name);
      setKcal(Math.round(res.estimate.kcal));
      setProtein(Math.round(res.estimate.protein_g));
      setCarbs(Math.round(res.estimate.carbs_g));
      setFat(Math.round(res.estimate.fat_g));
    } catch (e) {
      setError(
        e instanceof Error
          ? e.message
          : "Estimate failed; fill in the numbers by hand",
      );
    } finally {
      setBusy(false);
    }
  }

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const body: Record<string, unknown> = {
        name,
        slot,
        kcal: kcal || 0,
        protein_g: protein || 0,
        carbs_g: carbs || 0,
        fat_g: fat || 0,
        notes,
      };
      if (meal) {
        await api.updateMeal(meal.id, body);
      } else {
        body.date = date;
        if (photo) body.photo_id = photo.id;
        if (estimate && estimate.items.length > 0) {
          body.items = estimate.items.map((it) => ({
            name: it.name,
            qty: it.qty,
            unit: it.unit,
            kcal: it.kcal,
            protein_g: it.protein_g,
            carbs_g: it.carbs_g,
            fat_g: it.fat_g,
          }));
        }
        await api.createMeal(body);
      }
      onSaved();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not save");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Sheet
      open={open}
      onClose={onClose}
      title={meal ? "Edit meal" : "Log a meal"}
    >
      <form onSubmit={save} className="space-y-4">
        {!meal && (
          <div className="flex flex-wrap items-center gap-3">
            <PhotoUpload
              kind="food"
              date={date}
              capture
              label="Photo → estimate"
              onUploaded={analyze}
              className="btn-primary"
            />
            <span className="text-xs text-ink-500">or type it below</span>
          </div>
        )}
        {photo && (
          <AuthImage
            src={photo.thumb_url}
            alt="meal"
            className="h-24 w-24 rounded-xl object-cover"
          />
        )}
        {estimate && (
          <div className="rounded-xl border border-ink-800 bg-ink-850 p-3 text-xs text-ink-400">
            <div className="mb-1 font-medium text-ink-200">
              Estimated: {estimate.name || "meal"}
            </div>
            {estimate.items.map((it, i) => (
              <div key={i} className="flex justify-between">
                <span>
                  {it.name} · {it.qty} {it.unit}
                </span>
                <span>{Math.round(it.kcal)} kcal</span>
              </div>
            ))}
            {estimate.notes && (
              <div className="mt-1 italic">{estimate.notes}</div>
            )}
          </div>
        )}
        <Field label="Name">
          <input
            className="field"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Chicken rice bowl"
          />
        </Field>
        <div>
          <span className="label">Slot</span>
          <div className="flex gap-2">
            {slots.map((s) => (
              <button
                type="button"
                key={s}
                className={`chip ${slot === s ? "chip-active" : ""}`}
                onClick={() => setSlot(s)}
              >
                {s}
              </button>
            ))}
          </div>
        </div>
        <div className="grid grid-cols-4 gap-2">
          <Field label="kcal">
            <NumberInput value={kcal} onChange={setKcal} />
          </Field>
          <Field label="Protein">
            <NumberInput value={protein} onChange={setProtein} />
          </Field>
          <Field label="Carbs">
            <NumberInput value={carbs} onChange={setCarbs} />
          </Field>
          <Field label="Fat">
            <NumberInput value={fat} onChange={setFat} />
          </Field>
        </div>
        <Field label="Notes">
          <input
            className="field"
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
          />
        </Field>
        <ErrorText>{error}</ErrorText>
        <button className="btn-primary w-full" disabled={busy}>
          {busy ? "Working…" : meal ? "Save" : "Log meal"}
        </button>
      </form>
    </Sheet>
  );
}

function guessSlot(): Slot {
  const h = new Date().getHours();
  if (h < 11) return "breakfast";
  if (h < 15) return "lunch";
  if (h < 17) return "snack";
  if (h < 22) return "dinner";
  return "snack";
}

/** Body metrics for a day. */
export function MetricsSheet({
  open,
  onClose,
  date,
  metrics,
  unit,
  onSaved,
}: {
  open: boolean;
  onClose: () => void;
  date: string;
  metrics: Metrics;
  unit: "kg" | "lb";
  onSaved: (d: Day) => void;
}) {
  const { user } = useAuth();
  const waterUnit = waterConfig(user?.water_unit || "gal");
  const [weight, setWeight] = useState<number | "">("");
  const [fat, setFat] = useState<number | "">("");
  const [hr, setHr] = useState<number | "">("");
  const [sleep, setSleep] = useState<number | "">("");
  const [steps, setSteps] = useState<number | "">("");
  const [water, setWater] = useState<number | "">("");
  const [mood, setMood] = useState<number | "">("");
  const [energy, setEnergy] = useState<number | "">("");
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open) return;
    setWeight(
      metrics.weight_kg != null
        ? Math.round(fromKg(metrics.weight_kg, unit) * 10) / 10
        : "",
    );
    setFat(metrics.body_fat_pct ?? "");
    setHr(metrics.resting_hr ?? "");
    setSleep(metrics.sleep_hours ?? "");
    setSteps(metrics.steps ?? "");
    setWater(
      metrics.water_ml == null
        ? ""
        : Math.round((metrics.water_ml / waterUnit.ml) * 1000) / 1000,
    );
    setMood(metrics.mood ?? "");
    setEnergy(metrics.energy ?? "");
    setNote(metrics.note || "");
    setError("");
  }, [open, metrics, unit, waterUnit.ml]);

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const body: Record<string, unknown> = { note };
      const clear: string[] = [];
      const put = (k: string, v: number | "") =>
        v === "" ? clear.push(k) : (body[k] = v);
      put(
        "weight_kg",
        weight === "" ? "" : Math.round(toKg(weight, unit) * 100) / 100,
      );
      put("body_fat_pct", fat);
      put("resting_hr", hr);
      put("sleep_hours", sleep);
      put("steps", steps);
      put("water_ml", water === "" ? "" : Math.round(water * waterUnit.ml));
      put("mood", mood);
      put("energy", energy);
      if (clear.length) body.clear = clear;
      const day = await api.updateDay(date, body);
      onSaved(day);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not save");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Sheet open={open} onClose={onClose} title="Body metrics">
      <form onSubmit={save} className="space-y-4">
        <div className="grid grid-cols-2 gap-3">
          <Field label={`Weight (${unit})`}>
            <NumberInput value={weight} onChange={setWeight} step="0.1" />
          </Field>
          <Field label="Body fat %">
            <NumberInput value={fat} onChange={setFat} step="0.1" />
          </Field>
          <Field label="Resting HR">
            <NumberInput value={hr} onChange={setHr} step="1" />
          </Field>
          <Field label="Sleep (h)">
            <NumberInput value={sleep} onChange={setSleep} step="0.25" />
          </Field>
          <Field label="Steps">
            <NumberInput value={steps} onChange={setSteps} step="1" />
          </Field>
          <Field label={`Water total (${waterUnit.short})`}>
            <NumberInput value={water} onChange={setWater} step="any" />
          </Field>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <Scale label="Mood" value={mood} onChange={setMood} />
          <Scale label="Energy" value={energy} onChange={setEnergy} />
        </div>
        <Field label="Note">
          <textarea
            className="field"
            rows={3}
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder="How the day felt"
          />
        </Field>
        <ErrorText>{error}</ErrorText>
        <button className="btn-primary w-full" disabled={busy}>
          {busy ? "Saving…" : "Save"}
        </button>
      </form>
    </Sheet>
  );
}

function Scale({
  label,
  value,
  onChange,
}: {
  label: string;
  value: number | "";
  onChange: (v: number | "") => void;
}) {
  return (
    <div>
      <span className="label">{label}</span>
      <div className="flex gap-1">
        {[1, 2, 3, 4, 5].map((n) => (
          <button
            type="button"
            key={n}
            className={`chip flex-1 justify-center ${value === n ? "chip-active" : ""}`}
            onClick={() => onChange(value === n ? "" : n)}
          >
            {n}
          </button>
        ))}
      </div>
    </div>
  );
}

const kinds = [
  "strength",
  "cardio",
  "walk",
  "run",
  "cycle",
  "swim",
  "yoga",
  "hiit",
  "sport",
  "other",
];

/** Log a workout. */
export function WorkoutSheet({
  open,
  onClose,
  date,
  onSaved,
}: {
  open: boolean;
  onClose: () => void;
  date: string;
  onSaved: () => void;
}) {
  const [kind, setKind] = useState("walk");
  const [activity, setActivity] = useState("");
  const [minutes, setMinutes] = useState<number | "">(20);
  const [kcal, setKcal] = useState<number | "">("");
  const [distance, setDistance] = useState<number | "">("");
  const [hr, setHr] = useState<number | "">("");
  const [notes, setNotes] = useState("");
  const [startTime, setStartTime] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.createWorkout({
        date,
        kind,
        activity,
        minutes: minutes || 0,
        kcal: kcal === "" ? null : kcal,
        distance_km: distance === "" ? null : distance,
        avg_hr: hr === "" ? null : hr,
        notes,
        started_at: startTime
          ? new Date(`${date}T${startTime}`).toISOString()
          : null,
      });
      setActivity("");
      setNotes("");
      setKcal("");
      setDistance("");
      setHr("");
      setStartTime("");
      onSaved();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not save");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Sheet open={open} onClose={onClose} title="Log exercise">
      <form onSubmit={save} className="space-y-4">
        <div>
          <span className="label">Kind</span>
          <div className="flex flex-wrap gap-2">
            {kinds.map((k) => (
              <button
                type="button"
                key={k}
                className={`chip ${kind === k ? "chip-active" : ""}`}
                onClick={() => setKind(k)}
              >
                {k}
              </button>
            ))}
          </div>
        </div>
        <Field label="Activity (optional)">
          <input
            className="field"
            value={activity}
            onChange={(e) => setActivity(e.target.value)}
            placeholder="Push day, 5k tempo, evening walk…"
          />
        </Field>
        <div className="grid grid-cols-4 gap-2">
          {[10, 20, 30, 60].map((value) => (
            <button
              type="button"
              key={value}
              className={`btn-ghost px-2 ${minutes === value ? "chip-active" : ""}`}
              aria-pressed={minutes === value}
              onClick={() => setMinutes(value)}
            >
              {value} min
            </button>
          ))}
        </div>
        <Field label="Minutes">
          <NumberInput value={minutes} onChange={setMinutes} step="1" />
        </Field>
        <details className="rounded-xl border border-ink-800 p-3">
          <summary className="min-h-8 cursor-pointer text-sm text-ink-500">
            More details (optional)
          </summary>
          <div className="mt-3 space-y-3">
            <Field label="Started at (local time)">
              <input
                type="time"
                className="field"
                value={startTime}
                onChange={(e) => setStartTime(e.target.value)}
              />
            </Field>
            <div className="grid grid-cols-2 gap-3">
              <Field label="Calories">
                <NumberInput value={kcal} onChange={setKcal} step="1" />
              </Field>
              <Field label="Distance (km)">
                <NumberInput
                  value={distance}
                  onChange={setDistance}
                  step="0.1"
                />
              </Field>
              <Field label="Average heart rate">
                <NumberInput value={hr} onChange={setHr} step="1" />
              </Field>
            </div>
            <Field label="Notes">
              <textarea
                rows={2}
                className="field"
                value={notes}
                onChange={(e) => setNotes(e.target.value)}
              />
            </Field>
          </div>
        </details>
        <ErrorText>{error}</ErrorText>
        <button className="btn-primary w-full" disabled={busy || !minutes}>
          {busy ? "Saving…" : "Save exercise"}
        </button>
      </form>
    </Sheet>
  );
}

const styles = [
  "guided",
  "unguided",
  "breathwork",
  "body_scan",
  "walking",
  "other",
];

/** Log a meditation. */
export function MeditationSheet({
  open,
  onClose,
  date,
  onSaved,
}: {
  open: boolean;
  onClose: () => void;
  date: string;
  onSaved: () => void;
}) {
  const [minutes, setMinutes] = useState<number | "">(10);
  const [style, setStyle] = useState("guided");
  const [notes, setNotes] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.createMeditation({ date, minutes: minutes || 0, style, notes });
      setNotes("");
      onSaved();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not save");
    } finally {
      setBusy(false);
    }
  }
  return (
    <Sheet open={open} onClose={onClose} title="Log meditation">
      <form onSubmit={save} className="space-y-4">
        <div className="grid grid-cols-4 gap-2">
          {[5, 10, 15, 20].map((value) => (
            <button
              type="button"
              key={value}
              className={`btn-ghost px-2 ${minutes === value ? "chip-active" : ""}`}
              aria-pressed={minutes === value}
              onClick={() => setMinutes(value)}
            >
              {value} min
            </button>
          ))}
        </div>
        <Field label="Minutes">
          <NumberInput value={minutes} onChange={setMinutes} step="1" />
        </Field>
        <div>
          <span className="label">Style</span>
          <div className="flex flex-wrap gap-2">
            {styles.map((s) => (
              <button
                type="button"
                key={s}
                className={`chip ${style === s ? "chip-active" : ""}`}
                onClick={() => setStyle(s)}
              >
                {s.replace("_", " ")}
              </button>
            ))}
          </div>
        </div>
        <Field label="Notes">
          <input
            className="field"
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
          />
        </Field>
        <ErrorText>{error}</ErrorText>
        <button className="btn-primary w-full" disabled={busy || !minutes}>
          {busy ? "Saving…" : "Save meditation"}
        </button>
      </form>
    </Sheet>
  );
}

/** Write a journal entry. */
export function JournalSheet({
  open,
  onClose,
  date,
  onSaved,
}: {
  open: boolean;
  onClose: () => void;
  date: string;
  onSaved: () => void;
}) {
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      await api.createJournal({ date, title, body });
      setTitle("");
      setBody("");
      onSaved();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not save");
    } finally {
      setBusy(false);
    }
  }
  return (
    <Sheet open={open} onClose={onClose} title="Journal">
      <form onSubmit={save} className="space-y-4">
        <Field label="Title">
          <input
            className="field"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="optional"
          />
        </Field>
        <Field label="Entry">
          <textarea
            className="field"
            rows={8}
            value={body}
            onChange={(e) => setBody(e.target.value)}
            placeholder="What happened, how it felt, what you noticed."
          />
        </Field>
        <ErrorText>{error}</ErrorText>
        <button className="btn-primary w-full" disabled={busy || !body.trim()}>
          {busy ? "Saving…" : "Save entry"}
        </button>
      </form>
    </Sheet>
  );
}
