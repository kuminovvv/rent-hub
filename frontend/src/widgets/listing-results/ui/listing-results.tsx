"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { PointerEvent } from "react";
import { createPortal } from "react-dom";
import { ArrowUpRight, CalendarDays, Images, MapPin, X } from "lucide-react";
import Link from "next/link";
import { getBuildingRating, type MapListing, type TelegramPhoto, type TelegramPost } from "@/entities/listing";
import { ListingMap } from "@/widgets/listing-map";
import { formatAnnouncementCount } from "@/shared/lib/format-announcement-count";
import { CommutePanel } from "@/features/commute";

type ListingResultsProps = {
  posts: TelegramPost[];
  initialHasMore: boolean;
  initialCursor?: string;
  query: string;
  source: string;
  error: boolean;
  view: "list" | "map";
  mapItems: MapListing[];
  mapError: boolean;
  mapTotal: number;
  buildingType: "new" | "secondary" | "";
  rooms: Array<"0" | "1" | "2" | "3" | "4plus">;
  priceMin: string;
  priceMax: string;
  metro: string;
};

function photoUrl(photo: TelegramPhoto): string | null {
  if (photo.kind !== "photo" || typeof photo.url !== "string") return null;
  // Telegram photo IDs exceed JavaScript's safe integer range; retain the exact decimal ID.
  const match = /^\/api\/v1\/telegram\/media\/([1-9]\d*)$/.exec(photo.url);
  return match ? `/api/media/${match[1]}` : null;
}

function postPhotos(post: TelegramPost): string[] {
  if (!post.media) return [];
  const media = Array.isArray(post.media) ? post.media : [post.media];
  return media.map(photoUrl).filter((url): url is string => url !== null);
}

function formatDate(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "Дата не указана";
  return new Intl.DateTimeFormat("ru-RU", { day: "numeric", month: "long", year: "numeric" }).format(date);
}

function cleanText(text: string): string {
  return text.trim() || "В посте нет текстового описания.";
}

function formatMoney(value?: number | null): string | null {
  if (typeof value !== "number" || !Number.isFinite(value) || value <= 0) return null;
  return `${new Intl.NumberFormat("ru-RU").format(value)} ₽/мес.`;
}

function formatArea(value?: number | null): string | null {
  if (typeof value !== "number" || !Number.isFinite(value) || value <= 0) return null;
  return `${new Intl.NumberFormat("ru-RU", { maximumFractionDigits: 1 }).format(value)} м²`;
}

function nonEmptyText(value?: string | null): string | null {
  const text = value?.trim();
  return text || null;
}

function formatDeposit(value?: number | null): string | null {
  if (typeof value !== "number" || !Number.isFinite(value) || value <= 0) return null;
  return `${new Intl.NumberFormat("ru-RU").format(value)} ₽`;
}

function phoneHref(value: string): string | null {
  const trimmed = value.trim();
  if (!/^\+?[\d\s().-]+$/.test(trimmed)) return null;
  let digits = trimmed.replace(/\D/g, "");
  if (digits.length < 7 || digits.length > 15) return null;
  if (digits.length === 10) digits = `7${digits}`;
  else if (digits.length === 11 && digits.startsWith("8")) digits = `7${digits.slice(1)}`;
  return `tel:+${digits}`;
}

function socialHref(value: string, service: "telegram" | "vk"): string | null {
  const trimmed = value.trim();
  const handlePattern = service === "telegram" ? /^[A-Za-z0-9_]{5,32}$/ : /^[A-Za-z0-9_.]{2,64}$/;
  const host = service === "telegram" ? "t.me" : "vk.com";
  const handleFromUrl = (candidate: URL): string | null => {
    if (candidate.protocol !== "https:" || candidate.hostname.toLowerCase() !== host || candidate.search || candidate.hash) return null;
    const parts = candidate.pathname.split("/").filter(Boolean);
    return parts.length === 1 && handlePattern.test(parts[0]) ? parts[0] : null;
  };

  if (/^https:\/\//i.test(trimmed)) {
    try {
      const handle = handleFromUrl(new URL(trimmed));
      return handle ? `https://${host}/${encodeURIComponent(handle)}` : null;
    } catch {
      return null;
    }
  }
  const handle = trimmed.startsWith("@") ? trimmed.slice(1) : trimmed;
  return handlePattern.test(handle) ? `https://${host}/${encodeURIComponent(handle)}` : null;
}

const contactMethodLabels: Record<"phone" | "telegram" | "vk" | "other", string> = {
  phone: "Телефон",
  telegram: "Telegram",
  vk: "ВКонтакте",
  other: "Другой способ связи",
};

function formatRooms(value?: number | null): string | null {
  if (typeof value !== "number" || !Number.isFinite(value)) return null;
  return value === 0 ? "Студия" : `${value}-комнатная`;
}

function listingCardTitle(post: TelegramPost): string {
  const rooms = post.facts?.rooms;
  const area = formatArea(post.facts?.area_sqm);
  const apartment = typeof rooms === "number" && Number.isFinite(rooms)
    ? rooms === 0 ? "Студия" : `${rooms}-к. квартира`
    : "Квартира";
  return [apartment, area].filter(Boolean).join(", ");
}

function formatBuilding(value?: string | null): string | null {
  if (value === "new") return "Новостройка";
  if (value === "secondary") return "Вторичка";
  return null;
}

function formatBuildingFloors(min?: number, max?: number): string | null {
  if (typeof max !== "number" || max <= 0) return null;
  if (typeof min === "number" && min > 0 && min !== max) return `${min}–${max}`;
  return String(max);
}

function formatSourceDate(value?: string): string | null {
  if (!value) return null;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return null;
  return new Intl.DateTimeFormat("ru-RU", { day: "numeric", month: "long", year: "numeric" }).format(date);
}

function listingTitle(post: TelegramPost): string {
  return [formatMoney(post.facts?.price_rub), formatBuilding(post.facts?.building_type), formatRooms(post.facts?.rooms), formatArea(post.facts?.area_sqm)]
    .filter(Boolean)
    .join(", ") || "Объявление о квартире";
}

function ListingCard({ post, onOpen }: { post: TelegramPost; onOpen: (post: TelegramPost, button: HTMLButtonElement) => void }) {
  const photos = postPhotos(post);
  const [activePhotoIndex, setActivePhotoIndex] = useState(0);
  const title = listingTitle(post);
  const buildingRating = post.building ? getBuildingRating(post.building) : null;

  function handlePhotoPointerMove(event: PointerEvent<HTMLSpanElement>) {
    if (photos.length < 2 || event.pointerType === "touch") return;
    const { left, width } = event.currentTarget.getBoundingClientRect();
    const progress = Math.max(0, Math.min(0.999, (event.clientX - left) / width));
    setActivePhotoIndex(Math.floor(progress * photos.length));
  }

  return (
    <button
      aria-label={`Открыть подробности: ${title}`}
      className="listing-card"
      onClick={(event) => onOpen(post, event.currentTarget)}
      type="button"
    >
      <span
        className={`listing-card__media${photos.length > 0 ? " has-photo" : ""}`}
        onPointerLeave={() => setActivePhotoIndex(0)}
        onPointerMove={handlePhotoPointerMove}
      >
        {photos.length > 0 ? (
          <>
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img alt="Фото из объявления" loading="lazy" src={photos[activePhotoIndex]} />
            {photos.length > 1 ? (
              <>
                <span aria-hidden="true" className="listing-card__photo-progress">
                  {photos.map((photo, index) => (
                    <span className={index === activePhotoIndex ? "is-active" : ""} key={`${photo}-${index}`} />
                  ))}
                </span>
                <span className="listing-card__photo-count"><Images aria-hidden="true" />{photos.length}</span>
              </>
            ) : null}
          </>
        ) : (
          <span className="listing-card__placeholder"><Images aria-hidden="true" /><span>Фото в посте нет</span></span>
        )}
      </span>
      <span className="listing-card__body">
        <span className="listing-card__title">{listingCardTitle(post)}</span>
        {post.facts?.price_rub ? <span className="listing-card__price">{formatMoney(post.facts.price_rub)}</span> : null}
        {buildingRating ? <span className="listing-card__building-rating">Дом {buildingRating.score.toLocaleString("ru-RU", { maximumFractionDigits: 1 })}/10</span> : null}
        {post.facts?.address ? <span className="listing-card__address"><MapPin aria-hidden="true" />{post.facts.address}</span> : null}
      </span>
    </button>
  );
}

function ListingDetails({ post, loading, error, onClose, dimBackground }: { post: TelegramPost | null; loading: boolean; error: boolean; onClose: () => void; dimBackground: boolean }) {
  const closeButtonRef = useRef<HTMLButtonElement>(null);
  const photos = post ? postPhotos(post) : [];
  const facts = post?.facts;
  const price = formatMoney(facts?.price_rub);
  const rooms = formatRooms(facts?.rooms);
  const area = formatArea(facts?.area_sqm);
  const building = formatBuilding(facts?.building_type);
  const buildingInfo = post?.building;
  const buildingRating = buildingInfo ? getBuildingRating(buildingInfo) : null;
  const buildingFloors = formatBuildingFloors(buildingInfo?.floor_count_min, buildingInfo?.floor_count_max);
  const buildingSourceDate = formatSourceDate(buildingInfo?.source_dataset_date);
  const details = facts?.details;
  const floorLabel = typeof details?.floor === "number" && details.floor > 0
    ? "Этаж"
    : typeof details?.floors_total === "number" && details.floors_total > 0 ? "Этажей в доме" : null;
  const floorValue = typeof details?.floor === "number" && details.floor > 0
    ? `${details.floor}${typeof details.floors_total === "number" && details.floors_total > 0 ? ` из ${details.floors_total}` : ""}`
    : typeof details?.floors_total === "number" && details.floors_total > 0 ? String(details.floors_total) : null;
  const depositAmount = formatDeposit(details?.deposit_rub);
  const depositTerms = nonEmptyText(details?.deposit_terms);
  const utilitiesTerms = nonEmptyText(details?.utilities_terms);
  const commissionTerms = nonEmptyText(details?.commission_terms);
  const leaseTerm = nonEmptyText(details?.lease_term);
  const tenantRequirements = nonEmptyText(details?.tenant_requirements);
  const features = [...new Set((details?.features ?? []).map((feature) => feature.trim()).filter(Boolean))];
  const contactName = nonEmptyText(details?.contact_name);
  const contactPhone = nonEmptyText(details?.contact_phone);
  const contactTelegram = nonEmptyText(details?.contact_telegram);
  const contactVk = nonEmptyText(details?.contact_vk);
  const contactMethods = [...new Set(details?.contact_methods ?? [])];
  const hasTerms = Boolean(floorLabel || depositAmount || depositTerms || utilitiesTerms || commissionTerms || leaseTerm || tenantRequirements);
  const hasContacts = Boolean(contactName || contactPhone || contactTelegram || contactVk || contactMethods.length);

  useEffect(() => {
    closeButtonRef.current?.focus();
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") onClose();
      if (event.key === "Tab") {
        const panel = document.querySelector<HTMLElement>(".listing-sheet");
        const focusable = panel?.querySelectorAll<HTMLElement>('button, a[href], input, select, textarea, [tabindex]:not([tabindex="-1"])');
        if (!focusable?.length) return;
        const first = focusable[0];
        const last = focusable[focusable.length - 1];
        if (event.shiftKey && document.activeElement === first) {
          event.preventDefault();
          last.focus();
        } else if (!event.shiftKey && document.activeElement === last) {
          event.preventDefault();
          first.focus();
        }
      }
    }
    document.addEventListener("keydown", onKeyDown);
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", onKeyDown);
      document.body.style.overflow = "";
    };
  }, [onClose]);

  return (
    <div className={`listing-sheet__backdrop${dimBackground ? " is-dimmed" : ""}`} onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
      <section aria-labelledby="listing-sheet-title" aria-modal="true" className="listing-sheet" role="dialog">
        <header className="listing-sheet__header">
          <h2 id="listing-sheet-title">Подробности объявления</h2>
          <button aria-label="Закрыть подробности" className="listing-sheet__close" onClick={onClose} ref={closeButtonRef} type="button"><X aria-hidden="true" /></button>
        </header>
        <div className="listing-sheet__content">
          {photos.length > 0 ? (
            <div className={`listing-sheet__gallery${photos.length === 1 ? " is-single" : ""}`}>
              {photos.map((photo, index) => (
                // The media route is served by the local API proxy and is not a remote image host.
                // eslint-disable-next-line @next/next/no-img-element
                <img alt={`Фото квартиры из объявления ${index + 1} из ${photos.length}`} className="listing-sheet__photo" key={photo} loading={index === 0 ? "eager" : "lazy"} src={photo} />
              ))}
            </div>
          ) : null}
          {price ? <p className="listing-sheet__price">{price}</p> : null}
          <h3 className="listing-sheet__headline">{[building, rooms].filter(Boolean).join(" · ") || "Тип квартиры не указан"}</h3>
          {area ? <p className="listing-sheet__area">{area}</p> : null}
          <dl className="listing-sheet__facts">
            {facts?.address ? <div><dt>Адрес</dt><dd><MapPin aria-hidden="true" />{facts.address}</dd></div> : null}
            {facts?.metro ? <div><dt>Метро</dt><dd>{facts.metro}</dd></div> : null}
            {post ? <div><dt>Дата публикации</dt><dd><CalendarDays aria-hidden="true" />{formatDate(post.posted_at)}</dd></div> : null}
          </dl>
          {buildingInfo ? (
            <section className="listing-sheet__building" aria-labelledby="listing-building-title">
              <div className="listing-sheet__section-heading">
                <h3 id="listing-building-title">О доме</h3>
                {buildingInfo.is_alarm === true ? <span className="listing-sheet__warning">Аварийный</span> : null}
              </div>
              {buildingRating ? (
                <div className="listing-sheet__building-rating">
                  <p><strong>{buildingRating.score.toLocaleString("ru-RU", { maximumFractionDigits: 1 })}/10</strong> — ориентировочная оценка дома</p>
                  <ul>
                    {buildingRating.criteria.map((criterion) => (
                      <li key={criterion.key}><strong>{criterion.label}:</strong> {criterion.detail}</li>
                    ))}
                  </ul>
                  <p>Оценка рассчитана по подтверждённым техническим характеристикам ФРТ и не является официальной экспертизой состояния дома.</p>
                </div>
              ) : null}
              <dl className="listing-sheet__building-grid">
                {buildingInfo.built_year ? <div><dt>Год постройки</dt><dd>{buildingInfo.built_year}</dd></div> : null}
                {!buildingInfo.built_year && buildingInfo.exploitation_start_year ? <div><dt>Ввод в эксплуатацию</dt><dd>{buildingInfo.exploitation_start_year}</dd></div> : null}
                {buildingFloors ? <div><dt>Этажей</dt><dd>{buildingFloors}</dd></div> : null}
                {buildingInfo.house_type ? <div><dt>Тип дома</dt><dd>{buildingInfo.house_type}</dd></div> : null}
                {buildingInfo.wall_material ? <div><dt>Стены</dt><dd>{buildingInfo.wall_material}</dd></div> : null}
                {typeof buildingInfo.elevators_count === "number" ? <div><dt>Лифтов</dt><dd>{buildingInfo.elevators_count}</dd></div> : null}
                {typeof buildingInfo.entrance_count === "number" ? <div><dt>Подъездов</dt><dd>{buildingInfo.entrance_count}</dd></div> : null}
                {typeof buildingInfo.living_quarters_count === "number" ? <div><dt>Жилых помещений</dt><dd>{buildingInfo.living_quarters_count}</dd></div> : null}
                {buildingInfo.project_type ? <div><dt>Серия / проект</dt><dd>{buildingInfo.project_type}</dd></div> : null}
                {buildingInfo.floor_type ? <div><dt>Перекрытия</dt><dd>{buildingInfo.floor_type}</dd></div> : null}
                {buildingInfo.foundation_type ? <div><dt>Фундамент</dt><dd>{buildingInfo.foundation_type}</dd></div> : null}
                {buildingInfo.gas_type ? <div><dt>Газ</dt><dd>{buildingInfo.gas_type}</dd></div> : null}
                {buildingInfo.energy_efficiency ? <div><dt>Энергоэффективность</dt><dd>{buildingInfo.energy_efficiency}</dd></div> : null}
              </dl>
              <p className="listing-sheet__building-source">
                Источник: ФРТ{buildingSourceDate ? ` · данные на ${buildingSourceDate}` : ""}
              </p>
            </section>
          ) : null}
          <CommutePanel origin={{ latitude: facts?.latitude, longitude: facts?.longitude }} />
          {loading ? <p aria-live="polite" role="status">Загружаем подробности объявления…</p> : null}
          {error ? <p role="alert">Не удалось загрузить подробности объявления. Закройте панель и попробуйте ещё раз.</p> : null}
          {post ? (
            <>
              {hasTerms ? (
                <section className="listing-sheet__extracted" aria-labelledby="listing-terms-title">
                  <h3 id="listing-terms-title">Условия аренды</h3>
                  <dl>
                    {floorLabel && floorValue ? <div><dt>{floorLabel}</dt><dd>{floorValue}</dd></div> : null}
                    {depositAmount || depositTerms ? <div><dt>Залог</dt><dd>{[depositAmount, depositTerms].filter(Boolean).join(" · ")}</dd></div> : null}
                    {utilitiesTerms ? <div><dt>Коммунальные платежи</dt><dd>{utilitiesTerms}</dd></div> : null}
                    {commissionTerms ? <div><dt>Комиссия</dt><dd>{commissionTerms}</dd></div> : null}
                    {leaseTerm ? <div><dt>Срок аренды</dt><dd>{leaseTerm}</dd></div> : null}
                    {tenantRequirements ? <div><dt>Кого рассматривают</dt><dd>{tenantRequirements}</dd></div> : null}
                  </dl>
                </section>
              ) : null}
              {features.length ? (
                <section className="listing-sheet__extracted" aria-labelledby="listing-features-title">
                  <h3 id="listing-features-title">В квартире</h3>
                  <ul className="listing-sheet__features">
                    {features.map((feature) => <li key={feature}>{feature}</li>)}
                  </ul>
                </section>
              ) : null}
              {hasContacts ? (
                <section className="listing-sheet__extracted" aria-labelledby="listing-contacts-title">
                  <h3 id="listing-contacts-title">Контакты</h3>
                  <dl>
                    {contactName ? <div><dt>Имя</dt><dd>{contactName}</dd></div> : null}
                    {contactPhone ? <div><dt>Телефон</dt><dd>{phoneHref(contactPhone) ? <a href={phoneHref(contactPhone)!}>{contactPhone}</a> : contactPhone}</dd></div> : null}
                    {contactTelegram ? <div><dt>Telegram</dt><dd>{socialHref(contactTelegram, "telegram") ? <a href={socialHref(contactTelegram, "telegram")!} rel="noreferrer" target="_blank">{contactTelegram}</a> : contactTelegram}</dd></div> : null}
                    {contactVk ? <div><dt>ВКонтакте</dt><dd>{socialHref(contactVk, "vk") ? <a href={socialHref(contactVk, "vk")!} rel="noreferrer" target="_blank">{contactVk}</a> : contactVk}</dd></div> : null}
                    {contactMethods.length ? <div><dt>Способы связи</dt><dd>{contactMethods.map((method) => contactMethodLabels[method]).join(", ")}</dd></div> : null}
                  </dl>
                </section>
              ) : null}
              <div className="listing-sheet__description">
                <h3>Описание</h3>
                <p>{cleanText(post.text)}</p>
              </div>
              <a className="listing-sheet__original" href={post.source_url} rel="noreferrer" target="_blank">
                Открыть оригинал <ArrowUpRight aria-hidden="true" />
              </a>
            </>
          ) : null}
        </div>
      </section>
    </div>
  );
}

export function ListingResults({
  posts,
  initialHasMore,
  initialCursor,
  query,
  source,
  error,
  view,
  mapItems,
  mapError,
  mapTotal,
  buildingType,
  rooms,
  priceMin,
  priceMax,
  metro,
}: ListingResultsProps) {
  const [selectedPost, setSelectedPost] = useState<TelegramPost | null>(null);
  const [selectedMapKey, setSelectedMapKey] = useState<string | null>(null);
  const [isLoadingDetails, setIsLoadingDetails] = useState(false);
  const [detailsError, setDetailsError] = useState(false);
  const [loadedPosts, setLoadedPosts] = useState(posts);
  const [hasMore, setHasMore] = useState(initialHasMore);
  const [nextCursor, setNextCursor] = useState(initialCursor);
  const [isLoadingMore, setIsLoadingMore] = useState(false);
  const [loadMoreError, setLoadMoreError] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const sentinelRef = useRef<HTMLDivElement>(null);
  const loadingRef = useRef(false);
  const abortRef = useRef<AbortController | null>(null);
  const detailsAbortRef = useRef<AbortController | null>(null);
  const loadDetails = useCallback((channelId: number, messageId: number, initialPost: TelegramPost | null) => {
    detailsAbortRef.current?.abort();
    const controller = new AbortController();
    detailsAbortRef.current = controller;
    setSelectedPost(initialPost);
    setDetailsError(false);
    setIsLoadingDetails(true);

    void fetch(`/api/listing-detail/${channelId}/${messageId}`, { cache: "no-store", signal: controller.signal })
      .then(async (response) => {
        if (!response.ok) throw new Error("Could not load listing details");
        return await response.json() as TelegramPost;
      })
      .then((post) => {
        if (controller.signal.aborted) return;
        if (!post) throw new Error("Listing not found");
        setSelectedPost(post);
      })
      .catch(() => {
        if (!controller.signal.aborted && initialPost === null) setDetailsError(true);
      })
      .finally(() => {
        if (detailsAbortRef.current === controller) {
          detailsAbortRef.current = null;
          setIsLoadingDetails(false);
        }
      });
  }, []);
  const closeDetails = useCallback(() => {
    detailsAbortRef.current?.abort();
    detailsAbortRef.current = null;
    setSelectedPost(null);
    setSelectedMapKey(null);
    setIsLoadingDetails(false);
    setDetailsError(false);
    requestAnimationFrame(() => triggerRef.current?.focus());
  }, []);
  const selectMapListing = useCallback((key: string) => {
    const [channelRaw, messageRaw] = key.split(":");
    const channelId = Number(channelRaw);
    const messageId = Number(messageRaw);
    if (!Number.isSafeInteger(channelId) || channelId === 0 || !Number.isSafeInteger(messageId) || messageId <= 0) return;

    triggerRef.current = document.activeElement instanceof HTMLButtonElement ? document.activeElement : null;
    setSelectedMapKey(key);
    loadDetails(channelId, messageId, null);
  }, [loadDetails]);
  const hasFilters = Boolean(query || source || buildingType || rooms.length || priceMin || priceMax || metro);
  const pageSize = 24;
  const mapFilterQuery = useMemo(() => {
    const params = new URLSearchParams();
    if (query) params.set("q", query);
    if (source) params.set("source", source);
    if (buildingType) params.set("building_type", buildingType);
    for (const room of rooms) params.append("rooms", room);
    if (priceMin) params.set("price_min", priceMin);
    if (priceMax) params.set("price_max", priceMax);
    if (metro) params.set("metro", metro);
    return params.toString();
  }, [buildingType, metro, priceMax, priceMin, query, rooms, source]);

  const loadMore = useCallback(async () => {
    if (loadingRef.current || !hasMore || !nextCursor || view !== "list") return;
    loadingRef.current = true;
    setIsLoadingMore(true);
    setLoadMoreError(false);
    const controller = new AbortController();
    abortRef.current = controller;
    try {
      const params = new URLSearchParams(window.location.search);
      params.delete("page");
      params.set("limit", String(pageSize));
      params.set("cursor", nextCursor);
      const response = await fetch(`/api/listings?${params}`, { signal: controller.signal, cache: "no-store" });
      if (!response.ok) throw new Error("Failed to load listings");
      const page = await response.json() as { items: TelegramPost[]; has_more: boolean; next_cursor?: string };
      if (!Array.isArray(page.items) || typeof page.has_more !== "boolean") throw new Error("Invalid listings response");
      setLoadedPosts((current) => {
        const knownIds = new Set(current.map((post) => `${post.channel_id}:${post.message_id}`));
        return [...current, ...page.items.filter((post) => !knownIds.has(`${post.channel_id}:${post.message_id}`))];
      });
      setHasMore(page.has_more);
      setNextCursor(page.next_cursor);
    } catch (cause) {
      if (!(cause instanceof DOMException && cause.name === "AbortError")) setLoadMoreError(true);
    } finally {
      loadingRef.current = false;
      setIsLoadingMore(false);
      abortRef.current = null;
    }
  }, [hasMore, nextCursor, pageSize, view]);

  useEffect(() => () => {
    abortRef.current?.abort();
    detailsAbortRef.current?.abort();
  }, []);

  useEffect(() => {
    if (!hasMore || view !== "list" || !sentinelRef.current || typeof IntersectionObserver === "undefined") return;
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) void loadMore();
    }, { rootMargin: "700px 0px" });
    observer.observe(sentinelRef.current);
    return () => observer.disconnect();
  }, [hasMore, loadMore, view]);

  return (
    <section className="results-section" aria-labelledby="results-heading">
      <div className="results-heading">
        <h1 id="results-heading">Свежие объявления</h1>
      {!error && (view === "map" ? mapTotal : loadedPosts.length) > 0 ? <span className="results-count">{view === "map" ? formatAnnouncementCount(mapTotal) : `Показано: ${formatAnnouncementCount(loadedPosts.length)}`}</span> : null}
      </div>

      {view === "map" ? (
        <ListingMap items={mapItems} total={mapTotal} error={mapError} filterQuery={mapFilterQuery} selectedKey={selectedMapKey} onSelect={selectMapListing} />
      ) : error ? (
        <div className="results-state" role="status">
          <span className="results-state__icon"><Images aria-hidden="true" /></span>
          <h2>Объявления пока недоступны</h2>
          <p>Не удалось связаться с каталогом. Проверьте, что локальный backend запущен.</p>
        </div>
      ) : loadedPosts.length === 0 ? (
        <div className="results-state" role="status">
          <span className="results-state__icon"><Images aria-hidden="true" /></span>
          <h2>{hasFilters ? "Ничего не найдено" : "Пока нет объявлений"}</h2>
          <p>{hasFilters ? "Попробуйте изменить запрос или параметры фильтрации." : "Новые объявления появятся здесь после синхронизации."}</p>
          {hasFilters ? <Link className="results-state__reset" href="/">Сбросить фильтры</Link> : null}
        </div>
      ) : (
        <>
          <div className="listing-grid">
            {loadedPosts.map((post) => {
              return (
                <ListingCard
                  key={`${post.channel_id}:${post.message_id}`}
                  post={post}
                  onOpen={(selectedPost, button) => {
                    setSelectedMapKey(null);
                    triggerRef.current = button;
                    loadDetails(selectedPost.channel_id, selectedPost.message_id, selectedPost);
                  }}
                />
              );
            })}
          </div>
          <div aria-live="polite" className="listing-load-more" ref={sentinelRef}>
            {isLoadingMore ? <span>Загружаем объявления…</span> : null}
            {loadMoreError ? <button onClick={() => void loadMore()} type="button">Не удалось загрузить. Повторить</button> : null}
          </div>
        </>
      )}
      {(selectedPost || isLoadingDetails || detailsError) ? createPortal(<ListingDetails dimBackground={view === "list"} error={detailsError} loading={isLoadingDetails} onClose={closeDetails} post={selectedPost} />, document.body) : null}
    </section>
  );
}
