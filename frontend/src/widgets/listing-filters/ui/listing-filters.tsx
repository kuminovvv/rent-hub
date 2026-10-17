"use client";

import { useEffect, useRef, useState, useTransition, type ChangeEvent, type FormEvent } from "react";
import { ChevronDown, Filter, List, Map, X } from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { ListingSearch } from "@/features/listing-search";
import { CommuteDestinationFilter } from "@/features/commute";
import { Button } from "@/shared/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/shared/ui/popover";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/shared/ui/select";
import { Input } from "@/shared/ui/input";
import type { TelegramSource } from "@/entities/listing";

const buildingTypes = [
  { label: "Новостройка", value: "new" },
  { label: "Вторичка", value: "secondary" },
];

const roomOptions = [
  { label: "Студия", value: "0" },
  { label: "1", value: "1" },
  { label: "2", value: "2" },
  { label: "3", value: "3" },
  { label: "4+", value: "4plus" },
];

type ListingFiltersProps = {
  query: string;
  source: string;
  sources: TelegramSource[];
  view: "list" | "map";
  buildingType: "new" | "secondary" | "";
  rooms: Array<"0" | "1" | "2" | "3" | "4plus">;
  priceMin: string;
  priceMax: string;
  metro: string;
};

function renderRoomSelection(value: unknown) {
  if (!Array.isArray(value) || value.length === 0) return "Количество комнат";
  return value.map((room) => roomOptions.find((option) => option.value === room)?.label ?? room).join(", ");
}

function formatPrice(value: string) {
  return value.replace(/\D/g, "").replace(/\B(?=(\d{3})+(?!\d))/g, " ");
}

function formatCompactPrice(value: string) {
  const amount = Number(value);
  if (amount >= 1000) {
    return `${new Intl.NumberFormat("ru-RU", { maximumFractionDigits: 1 }).format(amount / 1000)} тыс. ₽`;
  }
  return `${formatPrice(value)} ₽`;
}

function formatPriceRange(min: string, max: string) {
  if (!min && !max) return "Цена";
  if (min && max && Number(min) >= 1000 && Number(max) >= 1000) {
    const minThousands = new Intl.NumberFormat("ru-RU", { maximumFractionDigits: 1 }).format(Number(min) / 1000);
    const maxThousands = new Intl.NumberFormat("ru-RU", { maximumFractionDigits: 1 }).format(Number(max) / 1000);
    return `${minThousands}—${maxThousands} тыс. ₽`;
  }
  return `${min ? `от ${formatCompactPrice(min)}` : ""}${min && max ? " · " : ""}${max ? `до ${formatCompactPrice(max)}` : ""}`;
}

function PriceAmountInput({ label, value, onChange }: { label: string; value: string; onChange: (value: string) => void }) {
  const inputRef = useRef<HTMLInputElement>(null);

  function handleChange(event: ChangeEvent<HTMLInputElement>) {
    const inputValue = event.currentTarget.value;
    const caret = event.currentTarget.selectionStart ?? inputValue.length;
    const digitsBeforeCaret = inputValue.slice(0, caret).replace(/\D/g, "").length;
    const digits = inputValue.replace(/\D/g, "");
    onChange(digits);

    requestAnimationFrame(() => {
      const input = inputRef.current;
      if (!input) return;

      const formattedValue = formatPrice(digits);
      let nextCaret = formattedValue.length;
      if (digitsBeforeCaret === 0) {
        nextCaret = 0;
      } else {
        let digitCount = 0;
        for (let index = 0; index < formattedValue.length; index += 1) {
          if (/\d/.test(formattedValue[index])) digitCount += 1;
          if (digitCount === digitsBeforeCaret) {
            nextCaret = index + 1;
            break;
          }
        }
      }
      input.setSelectionRange(nextCaret, nextCaret);
    });
  }

  return (
    <Input
      ref={inputRef}
      aria-label={`Цена ${label.toLowerCase()}`}
      className="price-filter-chip__input"
      inputMode="numeric"
      onChange={handleChange}
      placeholder="Любая"
      type="text"
      value={formatPrice(value)}
    />
  );
}

export function ListingFilters({
  query,
  source,
  sources,
  view,
  buildingType,
  rooms,
  priceMin,
  priceMax,
  metro,
}: ListingFiltersProps) {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [isPending, startTransition] = useTransition();
  const [isDebouncing, setIsDebouncing] = useState(false);
  const autoSubmitTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [priceMinValue, setPriceMinValue] = useState(priceMin);
  const [priceMaxValue, setPriceMaxValue] = useState(priceMax);

  useEffect(() => () => {
    if (autoSubmitTimer.current) clearTimeout(autoSubmitTimer.current);
  }, []);

  function navigateWithFilters(form: HTMLFormElement, submitter?: HTMLElement | null) {
    const formData = new FormData(form);
    const nextParams = new URLSearchParams();
    for (const [key, value] of formData.entries()) {
      if (typeof value === "string" && value.trim() !== "") nextParams.append(key, value.trim());
    }

    const submitControl = submitter as HTMLButtonElement | HTMLInputElement | null | undefined;
    if (submitControl?.name && submitControl.value) {
      nextParams.set(submitControl.name, submitControl.value);
    } else {
      const currentView = searchParams.get("view");
      if (currentView) nextParams.set("view", currentView);
    }

    const query = nextParams.toString();
    const href = query ? `/?${query}` : "/";
    if (`${window.location.pathname}${window.location.search}` === href) return;
    startTransition(() => router.replace(href, { scroll: false }));
  }

  function scheduleAutoSubmit() {
    if (autoSubmitTimer.current) clearTimeout(autoSubmitTimer.current);
    setIsDebouncing(true);
    autoSubmitTimer.current = setTimeout(() => {
      autoSubmitTimer.current = null;
      setIsDebouncing(false);
      const form = document.getElementById("listing-search");
      if (form instanceof HTMLFormElement) navigateWithFilters(form);
    }, 700);
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (autoSubmitTimer.current) clearTimeout(autoSubmitTimer.current);
    autoSubmitTimer.current = null;
    setIsDebouncing(false);
    navigateWithFilters(event.currentTarget, (event.nativeEvent as SubmitEvent).submitter);
  }

  return (
    <form action="/" className="filters-section" id="listing-search" method="get" onChange={scheduleAutoSubmit} onSubmit={handleSubmit} aria-label="Поиск объявлений">
      <div className="filter-toolbar">
        <Select
          defaultValue={buildingType || null}
          items={buildingTypes}
          name="building_type"
          onValueChange={scheduleAutoSubmit}
        >
          <SelectTrigger aria-label="Новостройка или вторичка" className="filter-select">
            <SelectValue placeholder="Новостройка или вторичка" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="new">Новостройка</SelectItem>
            <SelectItem value="secondary">Вторичка</SelectItem>
          </SelectContent>
        </Select>
        <Select
          defaultValue={rooms}
          items={roomOptions}
          multiple
          name="rooms"
          onValueChange={scheduleAutoSubmit}
        >
          <SelectTrigger aria-label="Количество комнат" className="filter-select filter-select--rooms">
            <SelectValue placeholder="Количество комнат">{renderRoomSelection}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="0">Студия</SelectItem>
            <SelectItem value="1">1</SelectItem>
            <SelectItem value="2">2</SelectItem>
            <SelectItem value="3">3</SelectItem>
            <SelectItem value="4plus">4+</SelectItem>
          </SelectContent>
        </Select>
        <Popover>
          <PopoverTrigger aria-label={formatPriceRange(priceMinValue, priceMaxValue)} className="filter-select price-filter-trigger" type="button">
            <span aria-hidden="true" className="price-filter-trigger__currency">₽</span>
            <span className="price-filter-trigger__value">{formatPriceRange(priceMinValue, priceMaxValue)}</span>
            <span aria-hidden="true" className="listing-select-icon"><ChevronDown /></span>
          </PopoverTrigger>
          <PopoverContent className="price-filter-popover">
            <div className="price-filter-popover__inputs">
              <div className="price-filter-chip">
                <span className="price-filter-chip__label">От</span>
                <PriceAmountInput label="от" onChange={setPriceMinValue} value={priceMinValue} />
                {priceMinValue && <span className="price-filter-chip__currency">₽</span>}
                {priceMinValue && <button aria-label="Очистить цену от" className="price-filter-chip__clear" onClick={() => { setPriceMinValue(""); scheduleAutoSubmit(); }} type="button"><X aria-hidden="true" /></button>}
              </div>
              <div className="price-filter-chip">
                <span className="price-filter-chip__label">До</span>
                <PriceAmountInput label="до" onChange={setPriceMaxValue} value={priceMaxValue} />
                {priceMaxValue && <span className="price-filter-chip__currency">₽</span>}
                {priceMaxValue && <button aria-label="Очистить цену до" className="price-filter-chip__clear" onClick={() => { setPriceMaxValue(""); scheduleAutoSubmit(); }} type="button"><X aria-hidden="true" /></button>}
              </div>
            </div>
          </PopoverContent>
        </Popover>
        <CommuteDestinationFilter />
        <div className="filter-toolbar__separator" aria-hidden="true" />
        <details className="filter-details">
          <summary className="filter-toolbar__hint" title="Раскрыть дополнительные параметры">
            <Filter aria-hidden="true" />
            <span>Все фильтры</span>
          </summary>
          <div className="filter-details__content">
            <p className="filter-details__note">Тип дома показывается, только если его удалось определить в объявлении.</p>
            <ListingSearch query={query} source={source} sources={sources} />
            <label className="filter-extra-field">
              <span>Метро</span>
              <input defaultValue={metro} name="metro" placeholder="Например, Пионерская" />
            </label>
          </div>
        </details>
        {view === "map" ? (
          <Button className="filter-toolbar__map" name="view" type="submit" value="list" variant="ghost">
            <List aria-hidden="true" /><span>Списком</span>
          </Button>
        ) : (
          <Button className="filter-toolbar__map" name="view" type="submit" value="map" variant="ghost">
            <Map aria-hidden="true" /><span>На карте</span>
          </Button>
        )}
      </div>
      <input form="listing-search" name="price_min" type="hidden" value={priceMinValue} />
      <input form="listing-search" name="price_max" type="hidden" value={priceMaxValue} />
      {isDebouncing || isPending ? <span className="filter-toolbar__updating" role="status">Обновляем объявления…</span> : null}
    </form>
  );
}
