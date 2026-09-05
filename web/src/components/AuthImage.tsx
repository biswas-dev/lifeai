import { useEffect, useState } from "react";
import { api } from "../lib/api";

/**
 * Photos sit behind bearer auth, so a plain <img src> cannot fetch them.
 * Fetch the bytes and render an object URL, revoked on unmount.
 */
export function AuthImage({
  src,
  alt,
  className,
  onClick,
}: {
  src: string;
  alt: string;
  className?: string;
  onClick?: () => void;
}) {
  const [url, setUrl] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    let active = true;
    let objectURL = "";
    api
      .photoObjectURL(src)
      .then((u) => {
        if (!active) {
          URL.revokeObjectURL(u);
          return;
        }
        objectURL = u;
        setUrl(u);
      })
      .catch(() => active && setFailed(true));
    return () => {
      active = false;
      if (objectURL) URL.revokeObjectURL(objectURL);
    };
  }, [src]);
  if (failed)
    return (
      <div
        className={`flex items-center justify-center bg-ink-850 text-xs text-ink-600 ${className || ""}`}
      >
        unavailable
      </div>
    );
  if (!url)
    return <div className={`animate-pulse bg-ink-850 ${className || ""}`} />;
  return (
    <img
      src={url}
      alt={alt}
      className={className}
      onClick={onClick}
      loading="lazy"
    />
  );
}
