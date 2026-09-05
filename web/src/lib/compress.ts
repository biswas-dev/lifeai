/**
 * Client-side image compression: a phone photo is 3-8MB and the server
 * downscales to 1600px anyway, so shrink it here before it hits the wire.
 */
const MAX_EDGE = 1600;
const QUALITY = 0.82;

export interface CompressResult {
  blob: Blob;
  originalBytes: number;
  bytes: number;
  width: number;
  height: number;
}

export async function compressImage(file: Blob): Promise<CompressResult> {
  const bitmap = await loadBitmap(file);
  const { width, height } = fit(bitmap.width, bitmap.height, MAX_EDGE);
  const canvas = document.createElement("canvas");
  canvas.width = width;
  canvas.height = height;
  const ctx = canvas.getContext("2d");
  if (!ctx) {
    return {
      blob: file,
      originalBytes: file.size,
      bytes: file.size,
      width: bitmap.width,
      height: bitmap.height,
    };
  }
  ctx.imageSmoothingEnabled = true;
  ctx.imageSmoothingQuality = "high";
  ctx.drawImage(bitmap, 0, 0, width, height);
  if ("close" in bitmap) bitmap.close();

  let blob = await toBlob(canvas, "image/webp", QUALITY);
  if (!blob || blob.type !== "image/webp") {
    blob = await toBlob(canvas, "image/jpeg", QUALITY);
  }
  if (!blob) {
    return {
      blob: file,
      originalBytes: file.size,
      bytes: file.size,
      width,
      height,
    };
  }
  if (blob.size >= file.size) {
    return {
      blob: file,
      originalBytes: file.size,
      bytes: file.size,
      width: bitmap.width,
      height: bitmap.height,
    };
  }
  return { blob, originalBytes: file.size, bytes: blob.size, width, height };
}

async function loadBitmap(file: Blob): Promise<ImageBitmap | HTMLImageElement> {
  if ("createImageBitmap" in window) {
    try {
      return await createImageBitmap(file, { imageOrientation: "from-image" });
    } catch {
      // fall through
    }
  }
  const url = URL.createObjectURL(file);
  try {
    const img = new Image();
    await new Promise<void>((resolve, reject) => {
      img.onload = () => resolve();
      img.onerror = () => reject(new Error("could not read that image"));
      img.src = url;
    });
    return img;
  } finally {
    setTimeout(() => URL.revokeObjectURL(url), 0);
  }
}

export function fit(w: number, h: number, edge: number) {
  if (w <= edge && h <= edge) return { width: w, height: h };
  return w >= h
    ? { width: edge, height: Math.round((h * edge) / w) }
    : { width: Math.round((w * edge) / h), height: edge };
}

function toBlob(
  canvas: HTMLCanvasElement,
  type: string,
  quality: number,
): Promise<Blob | null> {
  return new Promise((resolve) => canvas.toBlob(resolve, type, quality));
}
