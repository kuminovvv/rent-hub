import type {
  BuildingInfo,
  TelegramPhoto,
  TelegramPost,
  TelegramPostPage,
  TelegramSource,
  MapListingPage,
  MapBounds,
  ListingDetailsFacts,
} from "../model/types";

export type ListingApiErrorKind =
  | "configuration"
  | "network"
  | "http"
  | "invalid_response";

export class ListingApiError extends Error {
  constructor(
    message: string,
    readonly kind: ListingApiErrorKind,
    readonly status?: number,
  ) {
    super(message);
    this.name = "ListingApiError";
  }
}

export type GetPostsOptions = {
	channelId?: number;
	messageId?: number;
	q?: string;
  source?: string;
  buildingType?: "new" | "secondary";
  rooms?: Array<"0" | "1" | "2" | "3" | "4plus">;
  priceMin?: number;
  priceMax?: number;
  metro?: string;
  offerOnly?: boolean;
  hasText?: boolean;
  limit?: number;
  offset?: number;
  cursor?: string;
};

export type GetMapListingsOptions = GetPostsOptions & {
  bounds?: MapBounds;
  zoom?: number;
};

function getApiConfig() {
  const apiUrl = process.env.API_URL ?? "http://127.0.0.1:8082";
  const apiKey = process.env.TELEGRAM_API_KEY;

  if (!apiKey) {
    throw new ListingApiError(
      "Telegram API is not configured",
      "configuration",
    );
  }

  return { apiUrl: apiUrl.replace(/\/+$/, ""), apiKey };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function isPhoto(value: unknown): value is TelegramPhoto {
  return (
    isRecord(value) &&
    value.kind === "photo" &&
    typeof value.photo_id === "number" &&
    typeof value.url === "string" &&
    typeof value.width === "number" &&
    typeof value.height === "number" &&
    typeof value.bytes === "number"
  );
}

function isListingFacts(value: unknown): boolean {
  if (!isRecord(value)) return false;
  const optionalValues = [
    value.kind,
    value.building_type,
    value.rooms,
    value.price_rub,
    value.area_sqm,
    value.metro,
    value.address,
    value.latitude,
    value.longitude,
  ];
  return optionalValues.every((field) => field === undefined || field === null || typeof field === "string" || typeof field === "number") &&
    (value.details === undefined || value.details === null || isListingDetailsFacts(value.details));
}

function isListingDetailsFacts(value: unknown): value is ListingDetailsFacts {
  if (!isRecord(value)) return false;
  const numberFields = [value.floor, value.floors_total, value.deposit_rub];
  const stringFields = [
    value.deposit_terms,
    value.utilities_terms,
    value.commission_terms,
    value.lease_term,
    value.tenant_requirements,
    value.contact_name,
    value.contact_phone,
    value.contact_telegram,
    value.contact_vk,
  ];
  const contactMethods = ["phone", "telegram", "vk", "other"];
  return numberFields.every(optionalNumber) &&
    stringFields.every(optionalString) &&
    (value.contact_methods === undefined || value.contact_methods === null ||
      (Array.isArray(value.contact_methods) && value.contact_methods.every((method) => typeof method === "string" && contactMethods.includes(method)))) &&
    (value.features === undefined || value.features === null ||
      (Array.isArray(value.features) && value.features.every((feature) => typeof feature === "string")));
}

function optionalNumber(value: unknown): boolean {
  return value === undefined || value === null || typeof value === "number";
}

function optionalString(value: unknown): boolean {
  return value === undefined || value === null || typeof value === "string";
}

function isBuildingInfo(value: unknown): value is BuildingInfo {
  if (!isRecord(value)) return false;
  return (
    value.source === "frt" &&
    typeof value.house_guid === "string" &&
    typeof value.address === "string" &&
    typeof value.imported_at === "string" &&
    optionalNumber(value.source_house_id) &&
    optionalNumber(value.built_year) &&
    optionalNumber(value.exploitation_start_year) &&
    optionalNumber(value.floor_count_max) &&
    optionalNumber(value.floor_count_min) &&
    optionalNumber(value.entrance_count) &&
    optionalNumber(value.elevators_count) &&
    optionalNumber(value.quarters_count) &&
    optionalNumber(value.living_quarters_count) &&
    optionalString(value.project_type) &&
    optionalString(value.house_type) &&
    optionalString(value.energy_efficiency) &&
    optionalString(value.foundation_type) &&
    optionalString(value.floor_type) &&
    optionalString(value.wall_material) &&
    optionalString(value.gas_type) &&
    optionalString(value.source_dataset_date) &&
    (value.is_alarm === undefined || value.is_alarm === null || typeof value.is_alarm === "boolean")
  );
}

function isPost(value: unknown): value is TelegramPost {
  return (
    isRecord(value) &&
    typeof value.channel_id === "number" &&
    typeof value.message_id === "number" &&
    typeof value.source_username === "string" &&
    typeof value.source_title === "string" &&
    typeof value.source_url === "string" &&
    typeof value.text === "string" &&
    typeof value.posted_at === "string" &&
    (value.media === undefined || isPhoto(value.media) || (Array.isArray(value.media) && value.media.length > 0 && value.media.every(isPhoto))) &&
    (value.facts === undefined || value.facts === null || isListingFacts(value.facts)) &&
    (value.building === undefined || value.building === null || isBuildingInfo(value.building))
  );
}

function isMapListing(value: unknown): boolean {
  return (
    isRecord(value) &&
    typeof value.channel_id === "number" &&
    typeof value.message_id === "number" &&
    typeof value.source_url === "string" &&
    typeof value.source_title === "string" &&
    (value.price_rub === null || typeof value.price_rub === "number") &&
    (value.rooms === null || typeof value.rooms === "number") &&
    (value.building_type === null || typeof value.building_type === "string") &&
    (value.address === null || typeof value.address === "string") &&
    (value.latitude === null || typeof value.latitude === "number") &&
    (value.longitude === null || typeof value.longitude === "number")
  );
}

function isMapListingPage(value: unknown): value is MapListingPage {
  return (
    isRecord(value) &&
    Array.isArray(value.items) &&
    value.items.every(isMapListing) &&
    typeof value.total === "number" &&
    (value.truncated === undefined || typeof value.truncated === "boolean")
  );
}

function appendFilters(query: URLSearchParams, options: GetPostsOptions) {
	if (options.channelId !== undefined) query.set("channel_id", String(options.channelId));
	if (options.messageId !== undefined) query.set("message_id", String(options.messageId));
  if (options.q) query.set("q", options.q);
  if (options.source) query.set("source", options.source);
  if (options.buildingType) query.set("building_type", options.buildingType);
  for (const room of options.rooms ?? []) query.append("rooms", room);
  if (options.priceMin !== undefined) query.set("price_min", String(options.priceMin));
  if (options.priceMax !== undefined) query.set("price_max", String(options.priceMax));
  if (options.metro) query.set("metro", options.metro);
}

function isPostPage(value: unknown): value is TelegramPostPage {
  return (
    isRecord(value) &&
    Array.isArray(value.items) &&
    value.items.every(isPost) &&
    typeof value.limit === "number" &&
    typeof value.has_more === "boolean" &&
    (value.next_cursor === undefined || typeof value.next_cursor === "string") &&
    (value.offset === undefined || typeof value.offset === "number")
  );
}

function isSources(value: unknown): value is TelegramSource[] {
  return (
    Array.isArray(value) &&
    value.every(
      (source) =>
        isRecord(source) &&
        typeof source.channel_id === "number" &&
        typeof source.username === "string" &&
        typeof source.title === "string" &&
        typeof source.enabled === "boolean",
    )
  );
}

async function request<T>(
  path: string,
  validate: (value: unknown) => value is T,
  policy: { cache?: RequestCache; revalidate?: number } = {},
): Promise<T> {
  const { apiUrl, apiKey } = getApiConfig();
  let response: Response;

  try {
    response = await fetch(`${apiUrl}${path}`, {
      headers: { "X-API-Key": apiKey, Accept: "application/json" },
      cache: policy.cache ?? "no-store",
      next: policy.revalidate !== undefined ? { revalidate: policy.revalidate } : undefined,
    });
  } catch {
    throw new ListingApiError("Could not reach the listings API", "network");
  }

  if (!response.ok) {
    throw new ListingApiError(
      "The listings API returned an error",
      "http",
      response.status,
    );
  }

  let value: unknown;
  try {
    value = await response.json();
  } catch {
    throw new ListingApiError(
      "The listings API returned invalid JSON",
      "invalid_response",
      response.status,
    );
  }

  if (!validate(value)) {
    throw new ListingApiError(
      "The listings API returned an unexpected response",
      "invalid_response",
      response.status,
    );
  }

  return value;
}

export function getPosts(
  options: GetPostsOptions = {},
  policy: { cache?: RequestCache; revalidate?: number } = { cache: "force-cache", revalidate: 10 },
): Promise<TelegramPostPage> {
  const query = new URLSearchParams();
  if (options.offerOnly !== false) query.set("offer_only", "true");
  if (options.hasText) query.set("has_text", "true");
  appendFilters(query, options);
  if (options.limit !== undefined) query.set("limit", String(options.limit));
  if (options.offset !== undefined)
    query.set("offset", String(options.offset));
  if (options.cursor) query.set("cursor", options.cursor);

  const suffix = query.size > 0 ? `?${query.toString()}` : "";
  return request(`/api/v1/telegram/posts${suffix}`, isPostPage, policy);
}

export async function getPostById(channelId: number, messageId: number): Promise<TelegramPost | null> {
	const page = await getPosts({ channelId, messageId, limit: 1 }, { cache: "no-store" });
	return page.items[0] ?? null;
}

export function getBuildingForPost(channelId: number, messageId: number): Promise<BuildingInfo | null> {
  return request(
    `/api/v1/telegram/building/${channelId}/${messageId}`,
    (value): value is BuildingInfo | null => value === null || isBuildingInfo(value),
    { cache: "no-store" },
  );
}

export function getMapListings(options: GetMapListingsOptions = {}): Promise<MapListingPage> {
  const query = new URLSearchParams();
  query.set("offer_only", "true");
  appendFilters(query, options);
  if (options.bounds) {
    query.set("bbox", [options.bounds.west, options.bounds.south, options.bounds.east, options.bounds.north].join(","));
  }
  if (options.zoom !== undefined) query.set("zoom", String(options.zoom));
  return request(`/api/v1/telegram/map?${query.toString()}`, isMapListingPage);
}

export function getSources(): Promise<TelegramSource[]> {
  return request("/api/v1/telegram/sources", isSources, { cache: "force-cache", revalidate: 300 });
}
