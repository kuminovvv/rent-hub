"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { BriefcaseBusiness, GraduationCap, MapPin } from "lucide-react";
import { Button } from "@/shared/ui/button";
import styles from "./commute-panel.module.css";

type Coordinates = { latitude: number; longitude: number };
type ListingOrigin = { latitude: number | null | undefined; longitude: number | null | undefined };
type PlaceKind = "work" | "study" | "custom";
type SavedPlace = {
  id: string;
  kind: PlaceKind;
  title: string;
  address: string;
  latitude: number;
  longitude: number;
};
type RouteResult = {
  distance_m: number;
  duration_s: number;
  transfers: number | null;
  walking_s: number | null;
  approximate: true;
};
type CachedRoute = { savedAt: number; result: RouteResult };
type CommutePanelProps = {
  origin: ListingOrigin;
};

const PLACES_STORAGE_KEY = "spb-rent:commute:places:v1";
const ROUTES_STORAGE_KEY = "spb-rent:commute:routes:v1";
const CACHE_TTL_MS = 6 * 60 * 60 * 1000;

function isCoordinates(value: unknown): value is Coordinates {
  if (!value || typeof value !== "object") return false;
  const coordinates = value as Partial<Coordinates>;
  return Number.isFinite(coordinates.latitude) && Number.isFinite(coordinates.longitude);
}

function readPlaces(): SavedPlace[] {
  try {
    const parsed: unknown = JSON.parse(localStorage.getItem(PLACES_STORAGE_KEY) ?? "[]");
    if (!Array.isArray(parsed)) return [];
    return parsed.filter((place): place is SavedPlace => {
      if (!place || typeof place !== "object") return false;
      const candidate = place as Partial<SavedPlace>;
      return (
        typeof candidate.id === "string" &&
        (candidate.kind === "work" || candidate.kind === "study" || candidate.kind === "custom") &&
        typeof candidate.title === "string" &&
        typeof candidate.address === "string" &&
        isCoordinates(candidate)
      );
    });
  } catch {
    return [];
  }
}

function readRouteCache(): Record<string, CachedRoute> {
  try {
    const parsed: unknown = JSON.parse(localStorage.getItem(ROUTES_STORAGE_KEY) ?? "{}");
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return {};
    const now = Date.now();
    return Object.fromEntries(
      Object.entries(parsed).filter((entry): entry is [string, CachedRoute] => {
        const value = entry[1] as Partial<CachedRoute> | null;
        return Boolean(value && typeof value.savedAt === "number" && now - value.savedAt < CACHE_TTL_MS && value.result);
      }),
    );
  } catch {
    return {};
  }
}

function routeCacheKey(origin: Coordinates, destination: Coordinates) {
  return [origin.latitude, origin.longitude, destination.latitude, destination.longitude]
    .map((value) => value.toFixed(6))
    .join(":");
}

function formatDuration(seconds: number) {
  const minutes = Math.max(1, Math.round(seconds / 60));
  const hours = Math.floor(minutes / 60);
  const remainingMinutes = minutes % 60;
  return hours > 0 ? `${hours} ч ${remainingMinutes} мин` : `${minutes} мин`;
}

function formatDistance(meters: number) {
  return meters >= 1000 ? `${(meters / 1000).toFixed(1).replace(".", ",")} км` : `${Math.round(meters)} м`;
}

function getDifficulty(route: RouteResult) {
  const minutes = route.duration_s / 60;
  if (minutes > 50 || (route.transfers ?? 0) >= 3) return { label: "Долго", tone: "hard" as const };
  if (minutes <= 30 && (route.transfers === null || route.transfers <= 1) && (route.walking_s ?? 0) <= 10 * 60) {
    return { label: "Удобно", tone: "easy" as const };
  }
  return { label: "Приемлемо", tone: "moderate" as const };
}

function placeTitle(kind: PlaceKind, title: string) {
  if (kind === "work") return "Работа";
  if (kind === "study") return "Учёба";
  return title.trim() || "Моё место";
}

export function CommutePanel({ origin }: CommutePanelProps) {
  const [places, setPlaces] = useState<SavedPlace[]>([]);
  const [loadingPlaces, setLoadingPlaces] = useState(true);
  const [routeLoadingId, setRouteLoadingId] = useState<string | null>(null);
  const [routes, setRoutes] = useState<Record<string, RouteResult>>({});
  const [message, setMessage] = useState("");
  const inFlightRef = useRef(new Set<string>());
  const hasOrigin = Number.isFinite(origin.latitude) && Number.isFinite(origin.longitude);
  const listingCoordinates = useMemo<Coordinates | null>(() =>
    hasOrigin ? { latitude: origin.latitude as number, longitude: origin.longitude as number } : null,
  [hasOrigin, origin.latitude, origin.longitude]);

  useEffect(() => {
    const hydrate = () => {
      setPlaces(readPlaces());
      setLoadingPlaces(false);
    };
    queueMicrotask(hydrate);
    window.addEventListener("spb-rent:commute:places-changed", hydrate);
    return () => window.removeEventListener("spb-rent:commute:places-changed", hydrate);
  }, []);

  const calculateRoute = useCallback(async (place: SavedPlace) => {
    if (!listingCoordinates) return;
    const cacheKey = routeCacheKey(listingCoordinates, place);
    const cache = readRouteCache();
    const cached = cache[cacheKey];
    if (cached && Date.now() - cached.savedAt < CACHE_TTL_MS) {
      setRoutes((current) => ({ ...current, [place.id]: cached.result }));
      setMessage("");
      return;
    }
    if (inFlightRef.current.has(cacheKey)) return;

    inFlightRef.current.add(cacheKey);
    setRouteLoadingId(place.id);
    setMessage("");
    try {
      const response = await fetch("/api/commute/route", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ origin: listingCoordinates, destination: { latitude: place.latitude, longitude: place.longitude } }),
      });
      if (!response.ok) throw new Error(response.status === 429 ? "route-limit" : "route-unavailable");
      const route: unknown = await response.json();
      if (!route || typeof route !== "object") throw new Error("route-invalid");
      const result = route as RouteResult;
      if (!Number.isFinite(result.distance_m) || !Number.isFinite(result.duration_s)) throw new Error("route-invalid");
      const normalized: RouteResult = {
        distance_m: result.distance_m,
        duration_s: result.duration_s,
        transfers: Number.isFinite(result.transfers) ? result.transfers : null,
        walking_s: Number.isFinite(result.walking_s) ? result.walking_s : null,
        approximate: true,
      };
      setRoutes((current) => ({ ...current, [place.id]: normalized }));
      try {
        localStorage.setItem(ROUTES_STORAGE_KEY, JSON.stringify({ ...cache, [cacheKey]: { savedAt: Date.now(), result: normalized } }));
      } catch {
        // A route remains usable for this view even when browser storage is unavailable.
      }
    } catch (error) {
      setMessage(error instanceof Error && error.message === "route-limit"
        ? "Лимит запросов к Geoapify исчерпан. Попробуйте позже."
        : "Маршрут сейчас недоступен. Попробуйте позже.");
    } finally {
      inFlightRef.current.delete(cacheKey);
      setRouteLoadingId((current) => current === place.id ? null : current);
    }
  }, [listingCoordinates]);

  useEffect(() => {
    if (loadingPlaces || !listingCoordinates || places.length === 0) return;
    let cancelled = false;
    const timer = window.setTimeout(() => {
      void (async () => {
        for (const place of places) {
          if (cancelled) return;
          await calculateRoute(place);
        }
      })();
    }, 0);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [calculateRoute, listingCoordinates, loadingPlaces, places]);

  return (
    <section className={styles.panel} aria-labelledby="commute-panel-title">
      <div className={styles.heading}>
        <div>
          <p className={styles.eyebrow}>Путь от квартиры</p>
          <h2 id="commute-panel-title" className={styles.title}>Мои маршруты</h2>
        </div>
      </div>

      {!hasOrigin && <p className={styles.notice} role="status">Для этой квартиры пока нет координат, поэтому маршрут рассчитать нельзя.</p>}

      {loadingPlaces ? <p className={styles.muted}>Загружаем сохранённые места…</p> : null}

      {!loadingPlaces && places.length === 0 && (
        <div className={styles.empty}>
          <MapPin aria-hidden="true" />
          <p>Добавьте адрес назначения в основном фильтре, чтобы сравнить дорогу от квартиры.</p>
        </div>
      )}

      <div className={styles.placeList}>
        {places.map((place) => {
          const route = routes[place.id];
          const difficulty = route ? getDifficulty(route) : null;
          const Icon = place.kind === "work" ? BriefcaseBusiness : place.kind === "study" ? GraduationCap : MapPin;
          return (
            <article className={styles.place} key={place.id}>
              <div className={styles.placeTop}>
                <div className={styles.placeHeading}>
                  <span className={styles.placeIcon}><Icon aria-hidden="true" /></span>
                  <div className={styles.placeText}>
                    <h3>{placeTitle(place.kind, place.title)}</h3>
                    <p>{place.address}</p>
                  </div>
                </div>
              </div>
              {route && difficulty ? (
                <div className={styles.routeResult} aria-live="polite">
                  <strong>{formatDuration(route.duration_s)}</strong>
                  <span className={`${styles.difficulty} ${styles[difficulty.tone]}`}>{difficulty.label}</span>
                  <span>{formatDistance(route.distance_m)}</span>
                  {route.transfers !== null && <span>{route.transfers} пересадок</span>}
                  {route.walking_s !== null && <span>Пешком {formatDuration(route.walking_s)}</span>}
                  <p>Маршрут приблизительный</p>
                </div>
              ) : (
                <Button type="button" variant="outline" className={styles.routeButton} disabled={!hasOrigin || routeLoadingId !== null} onClick={() => calculateRoute(place)}>
                  {routeLoadingId === place.id ? "Считаем маршрут…" : "Рассчитать дорогу"}
                </Button>
              )}
            </article>
          );
        })}
      </div>

      {message && <p className={styles.message} role="status">{message}</p>}
      <p className={styles.privacy}>Адреса и координаты мест сохраняются в localStorage этого браузера. Для поиска маршрута координаты передаются сервису маршрутизации.</p>
    </section>
  );
}
