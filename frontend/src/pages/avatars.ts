import { useEffect, useMemo, useState } from "react";
import { type Api, request, requestBlob, useRead } from "../host";
import rawCatalog from "./avatar-catalog.json";

export type AvatarUpload = { avatar: string; contentType: string; size: number };

export type AvatarPackId = "china" | "global";

export type AvatarAgeGroup = "child" | "teen" | "young-adult" | "middle-aged" | "senior";

export type AvatarCatalogEntry = {
  id: string;
  ref: string;
  pack: AvatarPackId;
  name: string;
  gender: "male" | "female" | "other";
  age: number;
  ageGroup: AvatarAgeGroup;
};

export type AvatarCatalog = {
  version: number;
  packs: { id: AvatarPackId; title: string; count: number }[];
  items: AvatarCatalogEntry[];
};

export const avatarCatalog = rawCatalog as AvatarCatalog;

export const defaultAvatars = Array.from(
  { length: 20 },
  (_, index) => `default/global/gl-${String(index + 1).padStart(3, "0")}`,
);

export function useAvatarCatalog() {
  return avatarCatalog;
}

const blobCache = new Map<string, string>();
const inflight = new Map<string, Promise<string>>();

export function defaultAvatarFor(seed: string) {
  let hash = 2166136261;
  for (let i = 0; i < seed.length; i += 1) {
    hash ^= seed.charCodeAt(i);
    hash = Math.imul(hash, 16777619);
  }
  const index = (hash >>> 0) % defaultAvatars.length;
  return defaultAvatars[index];
}

export function normalizeAvatarRef(value?: string) {
  if (!value) return "";
  if (value.startsWith("default/")) {
    const rest = value.slice("default/".length);
    if (/^\d{2}$/.test(rest)) return `default/global/gl-0${rest}`;
  }
  return value;
}

export function presetAvatarUrl(value: string, size: 128 | 256 = 128) {
  const normalized = normalizeAvatarRef(value);
  const rest = normalized.slice("default/".length);
  const slash = rest.indexOf("/");
  if (slash > 0) {
    const pack = rest.slice(0, slash);
    const id = rest.slice(slash + 1);
    return `/avatars/${pack}/${size}/${id}.avif`;
  }
  return `/avatars/global/${size}/gl-001.avif`;
}

function loadBlobUrl(hash: string) {
  const cached = blobCache.get(hash);
  if (cached) return Promise.resolve(cached);
  const existing = inflight.get(hash);
  if (existing) return existing;
  const pending = requestBlob(`/v1/avatars/${encodeURIComponent(hash)}`)
    .then((blob) => {
      const url = URL.createObjectURL(blob);
      blobCache.set(hash, url);
      return url;
    })
    .finally(() => {
      inflight.delete(hash);
    });
  inflight.set(hash, pending);
  return pending;
}

export function useAvatarUrl(avatar?: string, seed?: string, size: 128 | 256 = 128) {
  const effective = normalizeAvatarRef(avatar) || (seed ? defaultAvatarFor(seed) : "");
  const hash = effective.startsWith("sha256:") ? effective.slice("sha256:".length) : "";
  const [resolved, setResolved] = useState<string>(() => (hash ? blobCache.get(hash) ?? "" : ""));

  useEffect(() => {
    if (!hash) {
      setResolved("");
      return;
    }
    const cached = blobCache.get(hash);
    if (cached) {
      setResolved(cached);
      return;
    }
    let alive = true;
    void loadBlobUrl(hash).then((url) => {
      if (alive) setResolved(url);
    }).catch(() => {
      if (alive) setResolved("");
    });
    return () => { alive = false; };
  }, [hash]);

  if (effective.startsWith("default/")) return presetAvatarUrl(effective, size);
  if (hash) return resolved || (seed ? presetAvatarUrl(defaultAvatarFor(seed), size) : undefined);
  return undefined;
}

type DirectoryEntry = Api.MemberOption & { avatar?: string };
type EnterpriseElement = Api.EnterpriseModel["elements"][number] & { avatar?: string };

export function useAvatarDirectory() {
  const members = useRead<DirectoryEntry[]>("/v1/member-directory");
  const model = useRead<Api.EnterpriseModel>("/v1/enterprise");
  return useMemo(() => {
    const byId = new Map<string, string>();
    const byName = new Map<string, string>();
    if (model.status === "ready") {
      for (const element of model.data.elements as EnterpriseElement[]) {
        if (element.stereotype !== "ActualPerson" || element.deleted) continue;
        const avatar = element.avatar || defaultAvatarFor(element.id);
        byId.set(element.id, avatar);
        if (element.name && !byName.has(element.name)) byName.set(element.name, avatar);
      }
    }
    if (members.status === "ready") {
      for (const member of members.data) {
        const avatar = member.avatar || defaultAvatarFor(member.id);
        byId.set(member.id, avatar);
        if (member.name) byName.set(member.name, avatar);
      }
    }
    return {
      of: (id?: string, name?: string) => (id && byId.get(id)) || (name && byName.get(name)) || (id || name ? defaultAvatarFor(id || name || "") : undefined),
    };
  }, [members, model]);
}

async function encodeSquareAvatar(file: File): Promise<Blob> {
  if (file.type === "image/avif" && file.size <= 512 * 1024) return file;
  try {
    const bitmap = await createImageBitmap(file);
    const side = Math.min(bitmap.width, bitmap.height);
    const sx = Math.floor((bitmap.width - side) / 2);
    const sy = Math.floor((bitmap.height - side) / 2);
    const canvas = document.createElement("canvas");
    canvas.width = 256;
    canvas.height = 256;
    const ctx = canvas.getContext("2d");
    if (!ctx) {
      bitmap.close();
      return file;
    }
    ctx.drawImage(bitmap, sx, sy, side, side, 0, 0, 256, 256);
    bitmap.close();
    const toBlob = (type: string, quality: number) =>
      new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, type, quality));
    const avif = await toBlob("image/avif", 0.8);
    if (avif && avif.type === "image/avif") return avif;
    const webp = await toBlob("image/webp", 0.86);
    if (webp && webp.type === "image/webp") return webp;
    return file;
  } catch {
    return file;
  }
}

export async function uploadAvatarFile(file: File): Promise<AvatarUpload> {
  const prepared = await encodeSquareAvatar(file);
  return request<AvatarUpload>("/v1/avatars", {
    body: prepared,
    headers: { "Content-Type": prepared.type || file.type || "application/octet-stream" },
  });
}
