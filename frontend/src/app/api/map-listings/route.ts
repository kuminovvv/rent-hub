import {
  getMapListings,
  ListingApiError,
  type GetMapListingsOptions,
  type MapBounds,
} from "@/entities/listing";

const roomValues = new Set(["0", "1", "2", "3", "4plus"]);

function optionalNonNegativeInteger(value: string | null): number | undefined {
  if (value === null || !/^\d+$/.test(value)) return undefined;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) ? parsed : undefined;
}

function parseBounds(raw: string | null): MapBounds | null {
  if (!raw) return null;
  const parts = raw.split(",").map((value) => Number(value));
  if (parts.length !== 4 || parts.some((value) => !Number.isFinite(value))) return null;
  const [west, south, east, north] = parts;
  if (west < -180 || east > 180 || south < -90 || north > 90 || west >= east || south >= north) return null;
  return { west, south, east, north };
}

export async function GET(request: Request) {
  const params = new URL(request.url).searchParams;
  const bounds = parseBounds(params.get("bbox"));
  const zoom = optionalNonNegativeInteger(params.get("zoom"));
  if (!bounds || zoom === undefined || zoom > 22) {
    return Response.json({ error: "Некорректная область карты" }, { status: 400 });
  }

  const buildingType = params.get("building_type");
  const rooms = params.getAll("rooms");
  if ((buildingType && buildingType !== "new" && buildingType !== "secondary") || rooms.some((room) => !roomValues.has(room))) {
    return Response.json({ error: "Некорректные параметры фильтрации" }, { status: 400 });
  }

  const priceMinValue = params.get("price_min");
  const priceMaxValue = params.get("price_max");
  const priceMin = optionalNonNegativeInteger(priceMinValue);
  const priceMax = optionalNonNegativeInteger(priceMaxValue);
  if ((priceMinValue !== null && priceMin === undefined) || (priceMaxValue !== null && priceMax === undefined)) {
    return Response.json({ error: "Некорректный диапазон цены" }, { status: 400 });
  }

  const filters: GetMapListingsOptions = {
    q: params.get("q")?.slice(0, 200) ?? undefined,
    source: params.get("source")?.slice(0, 100) ?? undefined,
    buildingType: buildingType === "new" || buildingType === "secondary" ? buildingType : undefined,
    rooms: rooms.length ? rooms as NonNullable<GetMapListingsOptions["rooms"]> : undefined,
    priceMin,
    priceMax,
    metro: params.get("metro")?.slice(0, 100) ?? undefined,
    bounds,
    zoom,
  };

  try {
    const page = await getMapListings(filters);
    return Response.json(page, { headers: { "Cache-Control": "private, no-store" } });
  } catch (error) {
    const status = error instanceof ListingApiError && error.kind === "http" ? 502 : 503;
    return Response.json({ error: "Не удалось загрузить карту" }, { status, headers: { "Cache-Control": "no-store" } });
  }
}
