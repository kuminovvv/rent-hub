"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import type * as Leaflet from "leaflet";
import type { MapBounds, MapListing } from "@/entities/listing";

type LeafletMapProps = {
  items: MapListing[];
  selectedKey: string | null;
  onSelect: (key: string) => void;
  onViewportChange: (bounds: MapBounds, zoom: number) => void;
};

type DisplayPoint = {
  key: string;
  latitude: number;
  longitude: number;
  count: number;
  item?: MapListing;
};

type MarkerState = {
  latitude: number;
  longitude: number;
  label: string;
  clustered: boolean;
  popupSignature: string | null;
};

function keyOf(item: MapListing) {
  return `${item.channel_id}:${item.message_id}`;
}

function validItems(items: MapListing[]) {
  return items.filter((item) =>
    typeof item.latitude === "number" &&
    Number.isFinite(item.latitude) &&
    item.latitude >= 59.5 && item.latitude <= 60.3 &&
    typeof item.longitude === "number" &&
    Number.isFinite(item.longitude) &&
    item.longitude >= 29.3 && item.longitude <= 31.3,
  );
}

function clusterCellSize(zoom: number) {
  if (zoom >= 14) return 0;
  if (zoom <= 9) return 0.08;
  if (zoom === 10) return 0.05;
  if (zoom === 11) return 0.03;
  if (zoom === 12) return 0.016;
  return 0.008;
}

function clusterItems(items: MapListing[], zoom: number): DisplayPoint[] {
  const cellSize = clusterCellSize(zoom);
  if (cellSize === 0) {
    return items.map((item) => ({
      key: keyOf(item),
      latitude: item.latitude as number,
      longitude: item.longitude as number,
      count: 1,
      item,
    }));
  }

  const cells = new Map<string, { latitude: number; longitude: number; items: MapListing[] }>();
  for (const item of items) {
    const latitude = item.latitude as number;
    const longitude = item.longitude as number;
    const cellKey = `${Math.floor(latitude / cellSize)}:${Math.floor(longitude / cellSize)}`;
    const cell = cells.get(cellKey) ?? { latitude: 0, longitude: 0, items: [] };
    cell.latitude += latitude;
    cell.longitude += longitude;
    cell.items.push(item);
    cells.set(cellKey, cell);
  }

  return Array.from(cells, ([cellKey, cell]) => {
    if (cell.items.length === 1) {
      const item = cell.items[0];
      return { key: keyOf(item), latitude: item.latitude as number, longitude: item.longitude as number, count: 1, item };
    }
    return {
      key: `cluster:${zoom}:${cellKey}`,
      latitude: cell.latitude / cell.items.length,
      longitude: cell.longitude / cell.items.length,
      count: cell.items.length,
    };
  });
}

function popupContent(item: MapListing): HTMLElement {
  const root = document.createElement("div");
  root.className = "map-popup";
  const address = document.createElement("strong");
  address.textContent = item.address || "Адрес в объявлении";
  root.append(address);

  const details = document.createElement("p");
  const price = item.price_rub ? `${new Intl.NumberFormat("ru-RU").format(item.price_rub)} ₽` : "Цена не указана";
  const rooms = item.rooms === 0 ? "Студия" : item.rooms ? `${item.rooms} комн.` : "Комнатность не указана";
  details.textContent = `${price} · ${rooms}`;
  root.append(details);

  const link = document.createElement("a");
  link.href = item.source_url;
  link.target = "_blank";
  link.rel = "noreferrer";
  link.textContent = "Открыть оригинал объявления";
  root.append(link);
  return root;
}

export function LeafletMap({ items, selectedKey, onSelect, onViewportChange }: LeafletMapProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const mapRef = useRef<Leaflet.Map | null>(null);
  const layerRef = useRef<Leaflet.LayerGroup | null>(null);
  const markersRef = useRef(new Map<string, Leaflet.Marker>());
  const markerStatesRef = useRef(new Map<string, MarkerState>());
  const leafletRef = useRef<typeof Leaflet | null>(null);
  const viewportChangeRef = useRef(onViewportChange);
  const selectRef = useRef(onSelect);
  const [ready, setReady] = useState(false);
  const [zoom, setZoom] = useState(11);
  const exactItems = useMemo(() => validItems(items), [items]);
  const displayPoints = useMemo(() => clusterItems(exactItems, zoom), [exactItems, zoom]);

  useEffect(() => {
    viewportChangeRef.current = onViewportChange;
  }, [onViewportChange]);

  useEffect(() => {
    selectRef.current = onSelect;
  }, [onSelect]);

  useEffect(() => {
    let cancelled = false;
    let viewportTimer: number | undefined;
    const markers = markersRef.current;
    const markerStates = markerStatesRef.current;

    void import("leaflet").then((L) => {
      if (cancelled || !containerRef.current || mapRef.current) return;
      leafletRef.current = L;
      const map = L.map(containerRef.current, {
        scrollWheelZoom: true,
        zoomControl: false,
        attributionControl: true,
      }).setView([59.9386, 30.3141], 11);
      map.attributionControl.setPrefix(false);
      L.tileLayer("https://tile.openstreetmap.org/{z}/{x}/{y}.png", {
        maxZoom: 19,
        attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap contributors</a>',
      }).addTo(map);
      mapRef.current = map;
      layerRef.current = L.layerGroup().addTo(map);
      const emitViewport = () => {
        if (viewportTimer !== undefined) window.clearTimeout(viewportTimer);
        viewportTimer = window.setTimeout(() => {
          if (cancelled) return;
          const currentZoom = map.getZoom();
          const bounds = map.getBounds();
          setZoom(currentZoom);
          viewportChangeRef.current({
            west: bounds.getWest(),
            south: bounds.getSouth(),
            east: bounds.getEast(),
            north: bounds.getNorth(),
          }, currentZoom);
        }, 120);
      };
      map.on("moveend", emitViewport);
      map.on("zoomend", emitViewport);
      setReady(true);
      window.setTimeout(() => {
        map.invalidateSize();
        emitViewport();
      }, 0);
    });

    return () => {
      cancelled = true;
      if (viewportTimer !== undefined) window.clearTimeout(viewportTimer);
      mapRef.current?.remove();
      mapRef.current = null;
      layerRef.current = null;
      markers.clear();
      markerStates.clear();
      leafletRef.current = null;
    };
  }, []);

  useEffect(() => {
    const map = mapRef.current;
    const container = containerRef.current;
    if (!ready || !map || !container || typeof ResizeObserver === "undefined") return;

    const observer = new ResizeObserver(() => map.invalidateSize({ pan: false }));
    observer.observe(container);
    return () => observer.disconnect();
  }, [ready]);

  useEffect(() => {
    const L = leafletRef.current;
    const map = mapRef.current;
    const layer = layerRef.current;
    if (!ready || !L || !map || !layer) return;

    const desiredKeys = new Set(displayPoints.map((point) => point.key));
    for (const [key, marker] of markersRef.current) {
      if (!desiredKeys.has(key)) {
        layer.removeLayer(marker);
        markersRef.current.delete(key);
        markerStatesRef.current.delete(key);
      }
    }

    displayPoints.forEach((point) => {
      const item = point.item;
      const price = item?.price_rub ? `${Math.round(item.price_rub / 1000)}k` : "•";
      const label = point.count > 1 ? String(point.count) : price;
      const clustered = point.count > 1;
      const popupSignature = item ? JSON.stringify([item.address, item.price_rub, item.rooms, item.source_url]) : null;
      const previous = markerStatesRef.current.get(point.key);
      let marker = markersRef.current.get(point.key);
      if (!marker) {
        const icon = L.divIcon({
          className: `listing-map__marker-icon${clustered ? " listing-map__marker-icon--cluster" : ""}`,
          html: `<span>${label}</span>`,
          iconSize: clustered ? [42, 42] : [48, 34],
          iconAnchor: clustered ? [21, 21] : [24, 34],
        });
        marker = L.marker([point.latitude, point.longitude], { icon }).addTo(layer);
        markersRef.current.set(point.key, marker);
        if (item) {
          marker.bindPopup(popupContent(item), { autoPan: false });
          marker.on("click", () => selectRef.current(point.key));
        } else {
          marker.on("click", () => map.setView(marker!.getLatLng(), Math.min(map.getZoom() + 2, 18)));
        }
      } else if (previous) {
        if (previous.latitude !== point.latitude || previous.longitude !== point.longitude) {
          marker.setLatLng([point.latitude, point.longitude]);
        }
        if (previous.label !== label || previous.clustered !== clustered) {
          marker.setIcon(L.divIcon({
            className: `listing-map__marker-icon${clustered ? " listing-map__marker-icon--cluster" : ""}`,
            html: `<span>${label}</span>`,
            iconSize: clustered ? [42, 42] : [48, 34],
            iconAnchor: clustered ? [21, 21] : [24, 34],
          }));
        }
        if (item && previous.popupSignature !== popupSignature) marker.setPopupContent(popupContent(item));
      }
      markerStatesRef.current.set(point.key, { latitude: point.latitude, longitude: point.longitude, label, clustered, popupSignature });
    });
  }, [displayPoints, ready]);

  useEffect(() => {
    const map = mapRef.current;
    if (!ready || !map) return;

    if (!selectedKey) {
      map.closePopup();
      return;
    }

    const selected = markersRef.current.get(selectedKey);
    if (!selected) return;
    if (selected.isPopupOpen()) return;
    const position = selected.getLatLng();
    if (!map.getBounds().contains(position)) map.panTo(position);
    selected.openPopup();
  }, [ready, selectedKey]);

  return <div aria-label="Карта объявлений с точными адресами" className="listing-map__canvas" ref={containerRef} role="application" />;
}
