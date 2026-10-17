"use client";

import { useEffect, useState, type ChangeEvent, type KeyboardEvent } from "react";
import { BriefcaseBusiness, GraduationCap, MapPin, Plus, Trash2 } from "lucide-react";
import { Button } from "@/shared/ui/button";
import { Input } from "@/shared/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/shared/ui/popover";

type PlaceKind = "work" | "study" | "custom";
type SavedPlace = {
  id: string;
  kind: PlaceKind;
  title: string;
  address: string;
  latitude: number;
  longitude: number;
};

const PLACES_STORAGE_KEY = "spb-rent:commute:places:v1";

function placeTitle(place: Pick<SavedPlace, "kind" | "title">) {
  if (place.kind === "work") return "Работа";
  if (place.kind === "study") return "Учёба";
  return place.title.trim() || "Моё место";
}

function readPlaces(): SavedPlace[] {
  try {
    const value: unknown = JSON.parse(localStorage.getItem(PLACES_STORAGE_KEY) ?? "[]");
    if (!Array.isArray(value)) return [];
    return value.filter((item): item is SavedPlace => {
      if (!item || typeof item !== "object") return false;
      const place = item as Partial<SavedPlace>;
      return typeof place.id === "string" &&
        (place.kind === "work" || place.kind === "study" || place.kind === "custom") &&
        typeof place.title === "string" && typeof place.address === "string" &&
        Number.isFinite(place.latitude) && Number.isFinite(place.longitude);
    });
  } catch {
    return [];
  }
}

export function CommuteDestinationFilter() {
  const [places, setPlaces] = useState<SavedPlace[]>([]);
  const [kind, setKind] = useState<PlaceKind>("work");
  const [title, setTitle] = useState("");
  const [address, setAddress] = useState("");
  const [selectedAddress, setSelectedAddress] = useState("");
  const [suggestions, setSuggestions] = useState<string[]>([]);
  const [activeSuggestion, setActiveSuggestion] = useState(-1);
  const [loadingSuggestions, setLoadingSuggestions] = useState(false);
  const [suggestionMessage, setSuggestionMessage] = useState("");
  const [open, setOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState("");

  useEffect(() => {
    queueMicrotask(() => setPlaces(readPlaces()));
  }, []);

  useEffect(() => {
    const query = address.trim();
    if (!open || query.length < 4 || query === selectedAddress) return;

    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      void fetch("/api/commute/autocomplete", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ query }),
        signal: controller.signal,
      })
        .then(async (response) => {
          if (!response.ok) throw new Error(response.status === 429 ? "limit" : "unavailable");
          const payload: unknown = await response.json();
          if (!payload || typeof payload !== "object") return [];
          const values = (payload as { suggestions?: unknown }).suggestions;
          if (!Array.isArray(values)) return [];
          return values.flatMap((item): string[] => {
            if (typeof item === "string") return [item];
            if (!item || typeof item !== "object") return [];
            const suggestion = (item as { address?: unknown }).address;
            return typeof suggestion === "string" ? [suggestion] : [];
          }).slice(0, 5);
        })
        .then((values) => {
          if (controller.signal.aborted) return;
          setSuggestions(values);
          setActiveSuggestion(values.length ? 0 : -1);
        })
        .catch((error: unknown) => {
          if (controller.signal.aborted) return;
          setSuggestions([]);
          setSuggestionMessage(error instanceof Error && error.message === "limit" ? "Лимит подсказок исчерпан. Попробуйте позже." : "Подсказки временно недоступны.");
        })
        .finally(() => {
          if (!controller.signal.aborted) setLoadingSuggestions(false);
        });
    }, 450);

    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [address, open, selectedAddress]);

  async function saveDestination() {
    const trimmedAddress = (selectedAddress || address).trim();
    const trimmedTitle = kind === "custom" ? title.trim() : placeTitle({ kind, title });
    if (!trimmedAddress || (kind === "custom" && !trimmedTitle)) {
      setMessage("Введите адрес места.");
      return;
    }
    if (places.length >= 5) {
      setMessage("Можно сохранить не более пяти мест.");
      return;
    }

    setSaving(true);
    setMessage("");
    try {
      const response = await fetch("/api/commute/geocode", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ address: trimmedAddress }),
      });
      if (!response.ok) throw new Error(response.status === 429 ? "limit" : "failed");
      const value: unknown = await response.json();
      if (!value || typeof value !== "object") throw new Error("invalid");
      const result = value as { address?: unknown; latitude?: unknown; longitude?: unknown };
      if (typeof result.latitude !== "number" || !Number.isFinite(result.latitude) || typeof result.longitude !== "number" || !Number.isFinite(result.longitude)) {
        throw new Error("invalid");
      }
      const next: SavedPlace[] = [...places, {
        id: crypto.randomUUID(),
        kind,
        title: trimmedTitle,
        address: typeof result.address === "string" && result.address ? result.address : trimmedAddress,
        latitude: result.latitude,
        longitude: result.longitude,
      }];
      localStorage.setItem(PLACES_STORAGE_KEY, JSON.stringify(next));
      window.dispatchEvent(new Event("spb-rent:commute:places-changed"));
      setPlaces(next);
      setAddress("");
      setSelectedAddress("");
      setSuggestions([]);
      setActiveSuggestion(-1);
      setTitle("");
      setMessage("Адрес сохранён в этом браузере.");
    } catch (error) {
      setMessage(error instanceof Error && error.message === "limit"
        ? "Лимит запросов к Geoapify исчерпан. Попробуйте позже."
        : "Не удалось найти адрес. Проверьте его и попробуйте ещё раз.");
    } finally {
      setSaving(false);
    }
  }

  function removePlace(id: string) {
    const next = places.filter((place) => place.id !== id);
    try {
      localStorage.setItem(PLACES_STORAGE_KEY, JSON.stringify(next));
    } catch {
      setMessage("Не удалось обновить сохранённые места браузера.");
      return;
    }
    window.dispatchEvent(new Event("spb-rent:commute:places-changed"));
    setPlaces(next);
    setMessage("Место удалено.");
  }

  function chooseSuggestion(value: string) {
    setAddress(value);
    setSelectedAddress(value);
    setSuggestions([]);
    setActiveSuggestion(-1);
    setLoadingSuggestions(false);
    setSuggestionMessage("");
  }

  function changeOpen(next: boolean) {
    setOpen(next);
    if (next && address.trim().length >= 4 && address.trim() !== selectedAddress) setLoadingSuggestions(true);
    if (!next) {
      setSuggestions([]);
      setActiveSuggestion(-1);
      setSuggestionMessage("");
      setLoadingSuggestions(false);
    }
  }

  function handleAddressKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "ArrowDown" && suggestions.length) {
      event.preventDefault();
      setActiveSuggestion((current) => (current + 1) % suggestions.length);
      return;
    }
    if (event.key === "ArrowUp" && suggestions.length) {
      event.preventDefault();
      setActiveSuggestion((current) => (current <= 0 ? suggestions.length - 1 : current - 1));
      return;
    }
    if (event.key === "Tab" && suggestions.length && selectedAddress !== address) {
      event.preventDefault();
      chooseSuggestion(suggestions[activeSuggestion >= 0 ? activeSuggestion : 0]);
      return;
    }
    if (event.key === "Enter") {
      event.preventDefault();
      if (suggestions.length && selectedAddress !== address) {
        chooseSuggestion(suggestions[activeSuggestion >= 0 ? activeSuggestion : 0]);
      } else {
        void saveDestination();
      }
    }
  }

  const triggerLabel = places.length ? `Маршрут: ${placeTitle(places[0])}` : "Маршрут до";

  return (
    <Popover open={open} onOpenChange={changeOpen}>
      <PopoverTrigger aria-label={triggerLabel} className="commute-filter-trigger" type="button">
        <MapPin aria-hidden="true" />
        <span>{triggerLabel}</span>
      </PopoverTrigger>
      <PopoverContent className="commute-filter-popover">
        <div className="commute-filter-popover__heading">
          <div>
            <strong>Куда вам нужно добираться?</strong>
            <p>Сравним дорогу от каждой квартиры</p>
          </div>
          <MapPin aria-hidden="true" />
        </div>
        <div className="commute-filter-popover__saved">
          {places.map((place) => {
            const Icon = place.kind === "work" ? BriefcaseBusiness : place.kind === "study" ? GraduationCap : MapPin;
            return (
              <div className="commute-filter-place" key={place.id}>
                <Icon aria-hidden="true" />
                <span><strong>{placeTitle(place)}</strong><small>{place.address}</small></span>
                <button aria-label={`Удалить место: ${placeTitle(place)}`} onClick={() => removePlace(place.id)} type="button"><Trash2 aria-hidden="true" /></button>
              </div>
            );
          })}
        </div>
        <div className="commute-filter-form" onChange={(event) => event.stopPropagation()}>
          <label className="commute-filter-field">
            <span>Место</span>
            <select value={kind} onChange={(event) => setKind(event.target.value as PlaceKind)}>
              <option value="work">Работа</option>
              <option value="study">Учёба</option>
              <option value="custom">Другое</option>
            </select>
          </label>
          {kind === "custom" && (
            <label className="commute-filter-field">
              <span>Название</span>
              <Input autoComplete="off" maxLength={60} onChange={(event: ChangeEvent<HTMLInputElement>) => setTitle(event.target.value)} placeholder="Например, спортзал" value={title} />
            </label>
          )}
          <label className="commute-filter-field commute-filter-field--address">
            <span>Адрес</span>
            <Input
              aria-activedescendant={activeSuggestion >= 0 ? `commute-suggestion-${activeSuggestion}` : undefined}
              aria-autocomplete="list"
              aria-controls="commute-address-suggestions"
              aria-expanded={suggestions.length > 0}
              autoComplete="street-address"
              maxLength={240}
              onChange={(event: ChangeEvent<HTMLInputElement>) => {
                const nextAddress = event.target.value;
                setAddress(nextAddress);
                setSelectedAddress("");
                setSuggestions([]);
                setActiveSuggestion(-1);
                setSuggestionMessage("");
                setLoadingSuggestions(open && nextAddress.trim().length >= 4);
              }}
              onKeyDown={handleAddressKeyDown}
              placeholder="Город, улица, дом"
              role="combobox"
              value={address}
            />
          </label>
          {suggestions.length > 0 && (
            <div aria-label="Подсказки адресов" className="commute-address-suggestions" id="commute-address-suggestions" role="listbox">
              {suggestions.map((suggestion, index) => (
                <button
                  aria-selected={index === activeSuggestion}
                  className={index === activeSuggestion ? "commute-address-suggestion commute-address-suggestion--active" : "commute-address-suggestion"}
                  id={`commute-suggestion-${index}`}
                  key={suggestion}
                  onClick={() => chooseSuggestion(suggestion)}
                  onMouseDown={(event) => event.preventDefault()}
                  role="option"
                  type="button"
                >
                  <MapPin aria-hidden="true" />{suggestion}
                </button>
              ))}
            </div>
          )}
          {loadingSuggestions ? <p className="commute-filter-message" role="status">Ищем адреса…</p> : null}
          {suggestionMessage ? <p className="commute-filter-message" role="status">{suggestionMessage}</p> : null}
          <Button className="commute-filter-save" disabled={saving} onClick={() => void saveDestination()} type="button">
            <Plus aria-hidden="true" />{saving ? "Ищем адрес…" : "Сохранить место"}
          </Button>
        </div>
        <p aria-live="polite" className="commute-filter-message" role={message.startsWith("Не удалось") || message.startsWith("Лимит") ? "alert" : "status"}>{message || "Адрес будет отправлен в Geoapify только после сохранения."}</p>
      </PopoverContent>
    </Popover>
  );
}
