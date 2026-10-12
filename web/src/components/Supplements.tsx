import { useState } from "react";
import { api } from "../lib/api";
import { compressImage } from "../lib/compress";
import { prettyDate } from "../lib/format";
import type { Day, Supplement, SupplementDay } from "../lib/types";
import { message } from "../lib/useResource";
import { AuthImage } from "./AuthImage";
import { CameraIcon, CheckIcon, PillIcon, PlusIcon } from "./Icons";
import { ErrorText } from "./ui";

const TAKEN_WITH = ["cold water", "water", "almond milk", "juice"];

/** Daily supplements: one tap marks each taken, with the usual drink. */
export function SupplementsCard({ day, onChanged }: { day: Day; onChanged: () => void }) {
  const list = day.supplements || [];
  const [busy, setBusy] = useState(0);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<Supplement | "new" | null>(null);

  async function toggle(sp: SupplementDay, takenWith = "") {
    setBusy(sp.id);
    setError("");
    try {
      if (sp.taken && !takenWith) await api.untakeSupplement(sp.id, day.date);
      else await api.takeSupplement(sp.id, day.date, takenWith);
      onChanged();
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(0);
    }
  }

  const done = list.filter((sp) => sp.taken).length;
  return (
    <section className="card overflow-hidden">
      <div className="flex items-center justify-between border-b border-ink-800 p-4">
        <div className="flex items-center gap-2">
          <PillIcon size={17} />
          <div>
            <h2 className="text-sm font-semibold">Supplements</h2>
            {list.length > 0 && (
              <p className="text-[11px] text-ink-500">
                {done}/{list.length} taken {day.is_today ? "today" : `on ${prettyDate(day.date)}`}
              </p>
            )}
          </div>
        </div>
        <button type="button" className="flex items-center gap-1 text-xs font-medium text-vital-500" onClick={() => setEditing("new")}>
          <PlusIcon size={14} /> Add
        </button>
      </div>
      <div className="space-y-2 p-4">
        <ErrorText>{error}</ErrorText>
        {list.length === 0 && !editing && (
          <p className="text-sm text-ink-400">Add what you take each day, then tick it off with one tap.</p>
        )}
        {list.map((sp) => (
          <div key={sp.id} className={`rounded-xl border p-3 ${sp.taken ? "border-vital-500/30 bg-vital-500/5" : "border-ink-800"}`}>
            <div className="flex items-center gap-3">
              <button type="button" className="shrink-0" onClick={() => setEditing(sp)} aria-label={`Edit ${sp.name}`}>
                {sp.image_url ? (
                  <AuthImage src={sp.image_url} alt={sp.product || sp.name} className="h-12 w-12 rounded-lg bg-white object-contain" />
                ) : (
                  <span className="flex h-12 w-12 items-center justify-center rounded-lg bg-ink-850 text-ink-500">
                    <PillIcon size={18} />
                  </span>
                )}
              </button>
              <div className="min-w-0 flex-1">
                <p className="truncate font-medium text-ink-100">{sp.name}</p>
                <p className="truncate text-[11px] text-ink-500">{[sp.product, sp.dose].filter(Boolean).join(" · ")}</p>
                <p className="text-[11px] text-ink-500">
                  {sp.taken ? `With ${sp.taken.taken_with || "—"}` : sp.streak ? `${sp.streak}-day streak` : "Not yet"}
                  {sp.days_since > 0 && ` · ${sp.days_taken}/${sp.days_since} days`}
                </p>
              </div>
              <button
                type="button"
                className={`flex h-10 shrink-0 items-center gap-1 rounded-xl px-3 text-xs font-medium ${sp.taken ? "bg-vital-500 text-white" : "border border-ink-800 bg-white text-ink-200"}`}
                disabled={busy !== 0}
                onClick={() => void toggle(sp)}
                aria-pressed={!!sp.taken}
                aria-label={sp.taken ? `${sp.name} taken; tap to undo` : `Mark ${sp.name} taken`}
              >
                {sp.taken ? (
                  <>
                    <CheckIcon size={14} /> Taken
                  </>
                ) : busy === sp.id ? (
                  "…"
                ) : (
                  "Take"
                )}
              </button>
            </div>
            {sp.taken && (
              <div className="mt-2 flex flex-wrap gap-1.5">
                {TAKEN_WITH.map((w) => (
                  <button
                    key={w}
                    type="button"
                    className={`rounded-full px-2.5 py-1 text-[11px] ${sp.taken?.taken_with === w ? "bg-vital-500/15 text-vital-500" : "bg-ink-850 text-ink-500"}`}
                    disabled={busy !== 0}
                    onClick={() => void toggle(sp, w)}
                  >
                    {w}
                  </button>
                ))}
              </div>
            )}
            {sp.missed.length > 0 && sp.missed.length <= 5 && (
              <p className="mt-1.5 text-[11px] text-ink-500">Missed: {sp.missed.map((d) => prettyDate(d)).join(", ")}</p>
            )}
          </div>
        ))}
        {editing && (
          <SupplementEditor
            supplement={editing === "new" ? null : editing}
            onClose={() => setEditing(null)}
            onSaved={() => {
              setEditing(null);
              onChanged();
            }}
          />
        )}
      </div>
    </section>
  );
}

function SupplementEditor({ supplement, onClose, onSaved }: { supplement: Supplement | null; onClose: () => void; onSaved: () => void }) {
  const [form, setForm] = useState({
    name: supplement?.name || "",
    product: supplement?.product || "",
    dose: supplement?.dose || "",
    taken_with: supplement?.taken_with || "cold water",
    product_url: supplement?.product_url || "",
    started_on: supplement?.started_on || "",
  });
  const [photo, setPhoto] = useState<File | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const set = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement>) => setForm((f) => ({ ...f, [k]: e.target.value }));

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const saved = await api.saveSupplement(supplement?.id || null, form);
      if (photo) {
        const { blob } = await compressImage(photo);
        await api.uploadSupplementImage(saved.id, blob);
      }
      onSaved();
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  }

  async function stop() {
    if (!supplement || !confirm(`Stop showing ${supplement.name} each day? Its history is kept.`)) return;
    setBusy(true);
    try {
      await api.saveSupplement(supplement.id, { active: false });
      onSaved();
    } catch (err) {
      setError(message(err));
      setBusy(false);
    }
  }

  return (
    <form onSubmit={(e) => void save(e)} className="space-y-2 rounded-xl border border-ink-800 p-3">
      <p className="text-xs font-semibold">{supplement ? `Edit ${supplement.name}` : "New supplement"}</p>
      <input className="field" placeholder="Name, e.g. Creatine" value={form.name} onChange={set("name")} required maxLength={80} />
      <input className="field" placeholder="Exact product (brand, flavour, size)" value={form.product} onChange={set("product")} maxLength={200} />
      <div className="grid grid-cols-2 gap-2">
        <input className="field" placeholder="Dose, e.g. 1 scoop (5 g)" value={form.dose} onChange={set("dose")} maxLength={80} />
        <input className="field" placeholder="Usually with…" value={form.taken_with} onChange={set("taken_with")} maxLength={80} />
      </div>
      <div className="grid grid-cols-2 gap-2">
        <label className="text-[11px] text-ink-500">
          Started
          <input className="field mt-1" type="date" value={form.started_on} onChange={set("started_on")} />
        </label>
        <label className="text-[11px] text-ink-500">
          Product link
          <input className="field mt-1" type="url" placeholder="https://" value={form.product_url} onChange={set("product_url")} />
        </label>
      </div>
      <label className="flex cursor-pointer items-center gap-2 text-xs text-ink-400">
        <CameraIcon size={16} />
        {photo ? photo.name : supplement?.image_url ? "Replace product photo" : "Add a product photo"}
        <input type="file" accept="image/*" className="hidden" onChange={(e) => setPhoto(e.target.files?.[0] || null)} />
      </label>
      <ErrorText>{error}</ErrorText>
      <div className="flex flex-wrap items-center gap-2 pt-1">
        <button type="submit" className="btn-primary btn-sm" disabled={busy}>
          {busy ? "Saving…" : "Save"}
        </button>
        <button type="button" className="btn-ghost btn-sm" onClick={onClose} disabled={busy}>
          Cancel
        </button>
        {supplement && (
          <button type="button" className="ml-auto text-xs text-ink-500 hover:text-rose-400" onClick={() => void stop()} disabled={busy}>
            Stop taking
          </button>
        )}
      </div>
    </form>
  );
}
