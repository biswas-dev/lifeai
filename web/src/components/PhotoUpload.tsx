import { useRef, useState } from "react";
import { api } from "../lib/api";
import { compressImage } from "../lib/compress";
import { formatBytes } from "../lib/format";
import type { Photo, Pose } from "../lib/types";
import { CameraIcon } from "./Icons";

/**
 * Picks (or captures) a photo, compresses it in the browser and uploads it.
 * The button reports the size saving because that is the whole reason the
 * compression exists.
 */
export function PhotoUpload({
  kind,
  date,
  pose,
  slot,
  autolog,
  label = "Add photo",
  capture,
  onUploaded,
  className = "btn-ghost",
}: {
  kind: "progress" | "food" | "ingredients";
  date?: string;
  pose?: Pose;
  slot?: string;
  autolog?: boolean;
  label?: string;
  capture?: boolean;
  onUploaded: (photo: Photo) => void;
  className?: string;
}) {
  const input = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState("");

  async function handle(file: File) {
    setBusy(true);
    setStatus("Compressing…");
    try {
      const c = await compressImage(file);
      setStatus(`Uploading ${formatBytes(c.bytes)}…`);
      const photo = await api.uploadPhoto(c.blob, {
        kind,
        date,
        pose,
        slot,
        autolog,
      });
      setStatus(
        c.bytes < c.originalBytes
          ? `${formatBytes(c.originalBytes)} → ${formatBytes(c.bytes)}`
          : "",
      );
      onUploaded(photo);
    } catch (e) {
      setStatus(e instanceof Error ? e.message : "upload failed");
    } finally {
      setBusy(false);
      if (input.current) input.current.value = "";
      setTimeout(() => setStatus(""), 4000);
    }
  }

  return (
    <div className="inline-flex flex-col items-start gap-1">
      <input
        ref={input}
        type="file"
        accept="image/*"
        capture={capture ? "environment" : undefined}
        className="hidden"
        onChange={(e) => e.target.files?.[0] && handle(e.target.files[0])}
      />
      <button
        type="button"
        className={className}
        disabled={busy}
        onClick={() => input.current?.click()}
      >
        <CameraIcon size={16} />
        {busy ? "Working…" : label}
      </button>
      {status && <span className="text-xs text-ink-500">{status}</span>}
    </div>
  );
}
