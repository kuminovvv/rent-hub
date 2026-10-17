import { getPosts, ListingApiError, type GetPostsOptions } from "@/entities/listing";

const PAGE_SIZE = 24;
const roomValues = new Set(["0", "1", "2", "3", "4plus"]);

function positiveOrZeroInteger(value: string | null): number | undefined {
  if (value === null || !/^\d+$/.test(value)) return undefined;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) ? parsed : undefined;
}

export async function GET(request: Request) {
  const params = new URL(request.url).searchParams;
  const cursor = params.get("cursor")?.trim() || undefined;
  if (cursor && (cursor.length > 160 || !/^\d+:-?\d+:\d+$/.test(cursor))) {
    return Response.json({ error: "Некорректный курсор страницы" }, { status: 400 });
  }

  const buildingType = params.get("building_type");
  const rooms = params.getAll("rooms");
  if ((buildingType && buildingType !== "new" && buildingType !== "secondary") || rooms.some((room) => !roomValues.has(room))) {
    return Response.json({ error: "Некорректные параметры фильтрации" }, { status: 400 });
  }

  const priceMinValue = params.get("price_min");
  const priceMaxValue = params.get("price_max");
  const priceMin = positiveOrZeroInteger(priceMinValue);
  const priceMax = positiveOrZeroInteger(priceMaxValue);
  if ((priceMinValue !== null && priceMin === undefined) || (priceMaxValue !== null && priceMax === undefined)) {
    return Response.json({ error: "Некорректный диапазон цены" }, { status: 400 });
  }

  const filters: GetPostsOptions = {
    q: params.get("q")?.slice(0, 200) ?? undefined,
    source: params.get("source")?.slice(0, 100) ?? undefined,
    buildingType: buildingType === "new" || buildingType === "secondary" ? buildingType : undefined,
    rooms: rooms.length ? rooms as NonNullable<GetPostsOptions["rooms"]> : undefined,
    priceMin,
    priceMax,
    metro: params.get("metro")?.slice(0, 100) ?? undefined,
    hasText: true,
    offerOnly: true,
    limit: PAGE_SIZE,
    cursor,
  };

  try {
    const page = await getPosts(filters);
    return Response.json(page, { headers: { "Cache-Control": "no-store" } });
  } catch (error) {
    const status = error instanceof ListingApiError && error.kind === "http" ? 502 : 503;
    return Response.json({ error: "Не удалось загрузить объявления" }, { status, headers: { "Cache-Control": "no-store" } });
  }
}
