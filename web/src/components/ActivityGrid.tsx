import { useEffect, useRef, useState, type ComponentType } from "react";
import { api } from "../lib/api";
import type { Day, WaterUnit } from "../lib/types";
import {
  drinkLabel,
  waterAmount,
  waterConfig,
  waterDisplay,
  waterUnits,
} from "../lib/water";
import { useAuth } from "../state/AuthContext";
import {
  CameraIcon,
  DropIcon,
  DumbbellIcon,
  LeafIcon,
  LotusIcon,
  PenIcon,
  PlusIcon,
} from "./Icons";
import { ErrorText, Sheet } from "./ui";
import { PhotoUpload } from "./PhotoUpload";

type LogKind = "meal" | "workout" | "meditation" | "journal";

/** A flexible daily log: tiles describe what happened, never a pass/fail rule. */
export function ActivityGrid({
  day,
  onLog,
  onChanged,
}: {
  day: Day;
  onLog: (kind: LogKind) => void;
  onChanged: (day?: Day) => void;
}) {
  const { user, refresh } = useAuth();
  const [unit, setUnit] = useState<WaterUnit>(user?.water_unit || "gal");
  const [waterOpen, setWaterOpen] = useState(false);
  const [photoOpen, setPhotoOpen] = useState(false);
  const [custom, setCustom] = useState("0.25");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const pending = useRef<{ intent: string; id: string } | null>(null);
  useEffect(() => {
    setUnit(user?.water_unit || "gal");
  }, [user?.water_unit]);
  useEffect(() => {
    setError("");
    setNotice("");
    pending.current = null;
  }, [day.date]);
  const water = day.metrics.water_ml || 0;
  const config = waterConfig(unit);

  async function add(amount: number) {
    const intent = `${day.date}:${amount}:${unit}`;
    if (!pending.current || pending.current.intent !== intent)
      pending.current = { intent, id: crypto.randomUUID() };
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const next = await api.addWater(
        day.date,
        amount,
        unit,
        pending.current.id,
      );
      pending.current = null;
      onChanged(next);
      setNotice(
        `${drinkLabel(amount, unit)} added. ${waterDisplay(next.metrics.water_ml, unit)} logged for the day.`,
      );
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Could not log water. Try again.",
      );
    } finally {
      setBusy(false);
    }
  }
  async function undo(id: number) {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      onChanged(await api.deleteWater(day.date, id));
      setNotice("Drink removed from your total.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not undo drink.");
    } finally {
      setBusy(false);
    }
  }
  async function chooseUnit(next: WaterUnit) {
    setBusy(true);
    setUnit(next);
    setCustom(String(waterConfig(next).quick[1]));
    setError("");
    try {
      await api.updateProfile({ water_unit: next });
      await refresh();
    } catch {
      setError(
        "Your unit preference could not be saved. You can still log water.",
      );
    } finally {
      setBusy(false);
    }
  }
  const entries = day.water || [];
  const progressPhotos = day.photos.filter((p) => p.kind === "progress");
  return (
    <section aria-labelledby="activity-grid-heading" className="space-y-3">
      <div className="flex items-end justify-between gap-3">
        <div>
          <h2
            id="activity-grid-heading"
            className="text-lg font-semibold tracking-tight"
          >
            Log as you go
          </h2>
          <p className="mt-1 text-xs text-ink-500">
            A little movement. A moment to yourself. Your own pace.
          </p>
        </div>
        <span className="hidden text-xs text-ink-500 sm:block">
          {day.is_today ? "Today" : day.date}
        </span>
      </div>
      <div className="grid grid-cols-2 gap-3" aria-label="Daily activities">
        <article className="activity-tile border-[#d7e7ed] bg-[#edf5f8]">
          <button
            type="button"
            className="activity-tile-main"
            onClick={() => {
              setCustom(String(config.quick[1]));
              setWaterOpen(true);
            }}
            aria-label="Log water"
          >
            <span className="activity-icon bg-white/80 text-[#417d94]">
              <DropIcon size={21} />
            </span>
            <span className="mt-3 block text-sm font-medium">Water</span>
            <span className="mt-1 block text-2xl font-semibold tabular-nums tracking-tight">
              {waterAmount(water, unit)}{" "}
              <span className="text-xs font-normal text-ink-500">
                {config.short}
              </span>
            </span>
            <span className="mt-1 block text-[11px] text-ink-500">
              {day.goals.water_ml
                ? `${waterDisplay(day.goals.water_ml, unit)} personal goal`
                : water > 0
                  ? "Every sip adds up"
                  : "Start with a sip"}
            </span>
          </button>
          <button
            type="button"
            className="activity-quick text-[#356e84]"
            disabled={busy}
            onClick={() => void add(config.quick[1])}
            aria-label={`Add ${drinkLabel(config.quick[1], unit)} of water`}
          >
            <PlusIcon size={14} />
            {busy ? "Saving…" : drinkLabel(config.quick[1], unit)}
          </button>
        </article>
        <ActivityTile
          title="Exercise"
          value={`${day.totals.workout_minutes}`}
          unit="min"
          detail={
            day.workouts.length
              ? `${day.workouts.length} ${day.workouts.length === 1 ? "session" : "sessions"} logged`
              : "A walk counts, too"
          }
          icon={DumbbellIcon}
          tone="bg-[#edf3ed] border-[#dae6da]"
          action="Log exercise"
          onClick={() => onLog("workout")}
        />
        <ActivityTile
          title="Meditation"
          value={`${day.totals.meditation_minutes}`}
          unit="min"
          detail={
            day.meditations.length
              ? `${day.meditations.length} ${day.meditations.length === 1 ? "sitting" : "sittings"} logged`
              : "Take a little pause"
          }
          icon={LotusIcon}
          tone="bg-[#f1eef7] border-[#e3ddec]"
          action="Log meditation"
          onClick={() => onLog("meditation")}
        />
        <ActivityTile
          title="Journal"
          value={`${day.journal.length}`}
          unit={day.journal.length === 1 ? "entry" : "entries"}
          detail={
            day.journal.length
              ? "Room for another thought"
              : "What’s on your mind?"
          }
          icon={PenIcon}
          tone="bg-[#f8f1e7] border-[#ebe0ce]"
          action="Write in journal"
          onClick={() => onLog("journal")}
        />
        <ActivityTile
          title="Food"
          value={`${day.meals.length}`}
          unit={day.meals.length === 1 ? "meal" : "meals"}
          detail={
            day.meals.length
              ? `${Math.round(day.totals.kcal).toLocaleString()} kcal recorded`
              : "Snap a plate or add a meal"
          }
          icon={LeafIcon}
          tone="bg-white border-ink-800"
          action="Log a meal"
          onClick={() => onLog("meal")}
        />
        <ActivityTile
          title="Photos"
          value={`${progressPhotos.length}`}
          unit={progressPhotos.length === 1 ? "photo" : "photos"}
          detail="A picture of your progress"
          icon={CameraIcon}
          tone="bg-white border-ink-800"
          action="Add progress photo"
          onClick={() => setPhotoOpen(true)}
        />
      </div>
      {!waterOpen && (
        <>
          <ErrorText>{error}</ErrorText>
          {notice && (
            <p role="status" className="text-xs text-vital-500">
              {notice}{" "}
              {entries[0] && (
                <button
                  className="ml-2 min-h-11 underline"
                  disabled={busy}
                  onClick={() => void undo(entries[0].id)}
                >
                  Undo last drink
                </button>
              )}
            </p>
          )}
        </>
      )}
      <Sheet
        open={waterOpen}
        onClose={() => setWaterOpen(false)}
        title="Log water"
      >
        <div className="space-y-5">
          <div className="rounded-2xl bg-[#edf5f8] p-5">
            <p className="text-xs text-ink-500">
              {day.is_today ? "Today" : day.date} · total logged
            </p>
            <p className="mt-2 text-4xl font-semibold tracking-tight text-[#356e84]">
              {waterDisplay(water, unit)}
            </p>
            {day.goals.water_ml && (
              <p className="mt-2 text-xs text-ink-500">
                Personal goal: {waterDisplay(day.goals.water_ml, unit)}
              </p>
            )}
          </div>
          <label className="block">
            <span className="label">Display water in</span>
            <select
              className="field"
              value={unit}
              onChange={(e) => void chooseUnit(e.target.value as WaterUnit)}
              disabled={busy}
            >
              {waterUnits.map((item) => (
                <option key={item.value} value={item.value}>
                  {item.label}
                </option>
              ))}
            </select>
          </label>
          <div className="grid grid-cols-3 gap-2">
            {config.quick.map((amount) => (
              <button
                type="button"
                key={amount}
                className="btn-ghost px-2"
                disabled={busy}
                onClick={() => void add(amount)}
              >
                + {drinkLabel(amount, unit)}
              </button>
            ))}
          </div>
          <form
            className="flex items-end gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              void add(Number(custom));
            }}
          >
            <label className="min-w-0 flex-1">
              <span className="label">Custom amount ({config.short})</span>
              <input
                className="field"
                type="number"
                inputMode="decimal"
                min="0.001"
                step="any"
                required
                value={custom}
                onChange={(e) => setCustom(e.target.value)}
              />
            </label>
            <button
              className="btn-primary min-h-11"
              disabled={busy || Number(custom) <= 0}
            >
              {busy ? "Saving…" : "Add water"}
            </button>
          </form>
          <p className="text-xs text-ink-500">
            Amounts add to your total. Gallons and fluid ounces use US units.
          </p>
          <ErrorText>{error}</ErrorText>
          {notice && (
            <p role="status" className="text-sm text-vital-500">
              {notice}
            </p>
          )}
          {entries.length > 0 && (
            <div>
              <h3 className="text-sm font-semibold">Drinks logged</h3>
              <ul className="mt-2 divide-y divide-ink-800">
                {entries.map((entry) => (
                  <li
                    key={entry.id}
                    className="flex min-h-12 items-center justify-between gap-3"
                  >
                    <span className="text-sm">
                      {waterDisplay(entry.amount_ml, unit)}
                    </span>
                    <button
                      type="button"
                      className="min-h-11 px-3 text-xs text-ink-500 underline"
                      disabled={busy}
                      onClick={() => void undo(entry.id)}
                      aria-label={`Undo drink ${entry.id}`}
                    >
                      Undo
                    </button>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
      </Sheet>
      <Sheet
        open={photoOpen}
        onClose={() => setPhotoOpen(false)}
        title="Add a progress photo"
      >
        <p className="mb-5 text-sm text-ink-500">
          A photo whenever you want to remember this moment.
        </p>
        <PhotoUpload
          kind="progress"
          date={day.date}
          pose="front"
          capture
          label="Take or choose a photo"
          onUploaded={() => {
            setPhotoOpen(false);
            onChanged();
          }}
          className="btn-primary w-full"
        />
      </Sheet>
    </section>
  );
}

function ActivityTile({
  title,
  value,
  unit,
  detail,
  icon: Icon,
  tone,
  action,
  onClick,
}: {
  title: string;
  value: string;
  unit: string;
  detail: string;
  icon: ComponentType<{ size?: number }>;
  tone: string;
  action: string;
  onClick: () => void;
}) {
  return (
    <article className={`activity-tile ${tone}`}>
      <button
        type="button"
        className="activity-tile-main"
        onClick={onClick}
        aria-label={action}
      >
        <span className="activity-icon bg-white/80 text-vital-500">
          <Icon size={21} />
        </span>
        <span className="mt-3 block text-sm font-medium">{title}</span>
        <span className="mt-1 block text-2xl font-semibold tabular-nums tracking-tight">
          {value}{" "}
          <span className="text-xs font-normal text-ink-500">{unit}</span>
        </span>
        <span className="mt-1 block text-[11px] text-ink-500">{detail}</span>
      </button>
      <button
        type="button"
        className="activity-quick"
        onClick={onClick}
        aria-label={`Add ${title.toLowerCase()} entry`}
      >
        <PlusIcon size={14} />
        {action}
      </button>
    </article>
  );
}
