"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { MapPin, ExternalLink } from "lucide-react";
import type { MapBounds, MapListing, MapListingPage } from "@/entities/listing";
import { LeafletMap } from "./leaflet-map";

type ListingMapProps = {
  items: MapListing[];
  total: number;
  error: boolean;
  filterQuery: string;
  selectedKey: string | null;
  onSelect: (key: string) => void;
};

function exactCoordinates(item: MapListing) {
  return typeof item.latitude === "number" && Number.isFinite(item.latitude) &&
    item.latitude >= 59.5 && item.latitude <= 60.3 &&
    typeof item.longitude === "number" && Number.isFinite(item.longitude) &&
    item.longitude >= 29.3 && item.longitude <= 31.3;
}

function label(item: MapListing) {
  if (item.rooms === 0) return "Студия";
  if (typeof item.rooms === "number") return `${item.rooms}-комнатная`;
  return "Квартира";
}

function price(value: number | null) {
  return value ? `${new Intl.NumberFormat("ru-RU").format(value)} ₽ / мес.` : "Цена не указана";
}

function pageSignature(page: MapListingPage) {
  return JSON.stringify([page.total, Boolean(page.truncated), page.items]);
}

export function ListingMap({ items, total, error, filterQuery, selectedKey, onSelect }: ListingMapProps) {
  const [visibleItems, setVisibleItems] = useState(items);
  const [visibleTotal, setVisibleTotal] = useState(total);
  const [liveError, setLiveError] = useState(error);
  const [truncated, setTruncated] = useState(false);
  const abortRef = useRef<AbortController | null>(null);
  const requestKeyRef = useRef<string | null>(null);
  const loadedRequestKeyRef = useRef<string | null>(null);
  const pageSignatureRef = useRef(JSON.stringify([total, false, items]));
  const mapped = useMemo(() => visibleItems.filter(exactCoordinates), [visibleItems]);
  const exactTotal = mapped.length;

  useEffect(() => () => abortRef.current?.abort(), []);

  const loadViewport = useCallback(async (bounds: MapBounds, zoom: number) => {
    const params = new URLSearchParams(filterQuery);
    params.set("bbox", [bounds.west, bounds.south, bounds.east, bounds.north].map((value) => value.toFixed(6)).join(","));
    params.set("zoom", String(zoom));
    const requestKey = params.toString();
    if (requestKeyRef.current === requestKey) return;
    if (loadedRequestKeyRef.current === requestKey) {
      abortRef.current?.abort();
      abortRef.current = null;
      requestKeyRef.current = null;
      return;
    }

    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;
    requestKeyRef.current = requestKey;
    try {
      const response = await fetch(`/api/map-listings?${requestKey}`, { cache: "no-store", signal: controller.signal });
      if (!response.ok) throw new Error("Failed to load map viewport");
      const page = await response.json() as MapListingPage;
      if (controller.signal.aborted || abortRef.current !== controller || requestKeyRef.current !== requestKey) return;
      if (!Array.isArray(page.items) || typeof page.total !== "number") throw new Error("Invalid map response");
      loadedRequestKeyRef.current = requestKey;
      const nextSignature = pageSignature(page);
      if (pageSignatureRef.current !== nextSignature) {
        pageSignatureRef.current = nextSignature;
        setVisibleItems(page.items);
        setVisibleTotal(page.total);
        setTruncated(Boolean(page.truncated));
      }
      setLiveError(false);
    } catch (cause) {
      if (!(cause instanceof DOMException && cause.name === "AbortError")) setLiveError(true);
    } finally {
      if (abortRef.current === controller) {
        abortRef.current = null;
        requestKeyRef.current = null;
      }
    }
  }, [filterQuery]);

  return (
    <div className="listing-map-layout">
      <div className="listing-map-layout__canvas"><LeafletMap items={mapped} selectedKey={selectedKey} onSelect={onSelect} onViewportChange={loadViewport} /></div>
      <aside className="listing-map-list" aria-label="Объявления на карте">
        <div className="listing-map-list__heading"><MapPin aria-hidden="true" /><span>{exactTotal}{truncated ? "+" : ""} на карте</span></div>
        {liveError ? <p className="listing-map-list__status" role="status">Не удалось обновить текущую область. Показаны последние загруженные точки.</p> : null}
        {!liveError && !exactTotal ? <p className="listing-map-list__status" role="status">{visibleTotal > 0 ? `В этой области нет объявлений с точными координатами. Подходящих объявлений: ${visibleTotal}.` : "В этой области подходящих объявлений пока нет."}</p> : null}
        <div className="listing-map-list__items">
          {mapped.map((item) => {
            const key = `${item.channel_id}:${item.message_id}`;
            return (
              <article className={`listing-map-list__item${selectedKey === key ? " is-selected" : ""}`} key={key}>
                <button className="listing-map-list__select" onClick={() => onSelect(key)} type="button">
                  <span className="listing-map-list__title">{label(item)} · {price(item.price_rub)}</span>
                  <span className="listing-map-list__address">{item.address || "Точный адрес указан в объявлении"}</span>
                </button>
                <a aria-label="Открыть оригинал объявления" className="listing-map-list__open" href={item.source_url} rel="noreferrer" target="_blank"><ExternalLink aria-hidden="true" /></a>
              </article>
            );
          })}
        </div>
      </aside>
    </div>
  );
}
