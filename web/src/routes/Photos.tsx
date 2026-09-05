import { useState } from "react";
import { api } from "../lib/api";
import type { Photo, Pose } from "../lib/types";
import { prettyDate } from "../lib/format";
import { message, useResource } from "../lib/useResource";
import { AuthImage } from "../components/AuthImage";
import { PhotoUpload } from "../components/PhotoUpload";
import {
  Empty,
  ErrorText,
  Field,
  PageHeader,
  Sheet,
  Spinner,
} from "../components/ui";

export function Photos() {
  const [kind, setKind] = useState<Photo["kind"]>("progress");
  const [pose, setPose] = useState<Pose>("");
  const photos = useResource(() => api.listPhotos(kind, pose), [kind, pose]);
  const [selected, setSelected] = useState<Photo | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  return (
    <div className="mx-auto max-w-3xl">
      <PageHeader
        title="Photo journal"
        subtitle="A visual record, one day at a time."
        action={
          <PhotoUpload kind={kind} pose={pose} onUploaded={photos.reload} />
        }
      />
      <div className="mb-5 flex flex-wrap gap-2">
        {(["progress", "food", "ingredients"] as const).map((k) => (
          <button
            className={kind === k ? "chip chip-active" : "chip"}
            aria-pressed={kind === k}
            key={k}
            onClick={() => {
              setKind(k);
              setPose("");
            }}
          >
            {k === "progress"
              ? "Body progress"
              : k === "food"
                ? "Meals"
                : "Ingredients"}
          </button>
        ))}
        {kind === "progress" && (
          <select
            aria-label="Filter by pose"
            className="field ml-auto w-auto py-1"
            value={pose}
            onChange={(e) => setPose(e.target.value as Pose)}
          >
            <option value="">All poses</option>
            {["front", "side", "back"].map((p) => (
              <option key={p}>{p}</option>
            ))}
          </select>
        )}
      </div>
      <ErrorText>{photos.error || error}</ErrorText>
      {!photos.data && !photos.error ? (
        <Spinner />
      ) : photos.data?.length === 0 ? (
        <Empty>Add a photo to start your visual record.</Empty>
      ) : (
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
          {photos.data?.map((p) => (
            <button
              key={p.id}
              className="card overflow-hidden text-left"
              onClick={() => setSelected(p)}
            >
              <AuthImage
                src={p.thumb_url || p.url}
                alt={p.caption || `${p.kind} on ${p.date}`}
                className="aspect-square w-full object-cover"
              />
              <div className="p-3 text-xs text-ink-400">
                {prettyDate(p.date)} · {p.pose || p.kind}
                <p className="mt-1 truncate text-ink-500">
                  {p.caption || p.source}
                </p>
              </div>
            </button>
          ))}
        </div>
      )}
      {selected && (
        <Sheet
          open
          title="Photo details"
          wide
          onClose={() => setSelected(null)}
        >
          <div className="space-y-4">
            <AuthImage
              src={selected.url}
              alt={selected.caption || selected.kind}
              className="max-h-[50vh] w-full rounded-xl object-contain"
            />
            <Field label="Caption">
              <input
                className="field"
                value={selected.caption}
                onChange={(e) =>
                  setSelected({ ...selected, caption: e.target.value })
                }
              />
            </Field>
            <Field label="Date">
              <input
                className="field"
                type="date"
                value={selected.date}
                onChange={(e) =>
                  setSelected({ ...selected, date: e.target.value })
                }
              />
            </Field>
            {selected.kind === "progress" && (
              <Field label="Pose">
                <select
                  className="field"
                  value={selected.pose}
                  onChange={(e) =>
                    setSelected({ ...selected, pose: e.target.value as Pose })
                  }
                >
                  <option value="">Unspecified</option>
                  {["front", "side", "back"].map((p) => (
                    <option key={p}>{p}</option>
                  ))}
                </select>
              </Field>
            )}
            <ErrorText>{error}</ErrorText>
            <div className="flex justify-between">
              <button
                className="btn-primary"
                disabled={busy}
                onClick={async () => {
                  setBusy(true);
                  setError("");
                  try {
                    await api.updatePhoto(selected.id, {
                      caption: selected.caption,
                      date: selected.date,
                      pose: selected.pose,
                    });
                    setSelected(null);
                    photos.reload();
                  } catch (e) {
                    setError(message(e));
                  } finally {
                    setBusy(false);
                  }
                }}
              >
                Save
              </button>
              <button
                className="btn-danger"
                disabled={busy}
                onClick={async () => {
                  if (!window.confirm("Delete this photo?")) return;
                  setBusy(true);
                  setError("");
                  try {
                    await api.deletePhoto(selected.id);
                    setSelected(null);
                    photos.reload();
                  } catch (e) {
                    setError(message(e));
                  } finally {
                    setBusy(false);
                  }
                }}
              >
                Delete
              </button>
            </div>
          </div>
        </Sheet>
      )}
    </div>
  );
}
