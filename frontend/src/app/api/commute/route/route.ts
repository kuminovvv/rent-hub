type Coordinate = { latitude?: unknown; longitude?: unknown };
type CommuteRequest = { origin?: Coordinate; destination?: Coordinate };

function isCoordinate(value: Coordinate | undefined): value is { latitude: number; longitude: number } {
  return typeof value?.latitude === "number" && Number.isFinite(value.latitude) &&
    typeof value.longitude === "number" && Number.isFinite(value.longitude);
}

function upstreamConfig() {
  const baseURL = (process.env.API_URL ?? "http://127.0.0.1:8082").replace(/\/+$/, "");
  const apiKey = process.env.TELEGRAM_API_KEY;
  if (!apiKey) return null;
  return { baseURL, apiKey };
}

export async function POST(request: Request) {
  let body: CommuteRequest;
  try {
    body = await request.json() as CommuteRequest;
  } catch {
    return Response.json({ error: "Некорректный запрос" }, { status: 400 });
  }

  if (!isCoordinate(body.origin) || !isCoordinate(body.destination)) {
    return Response.json({ error: "Для расчёта нужны координаты квартиры и места назначения" }, { status: 400 });
  }

  const config = upstreamConfig();
  if (!config) return Response.json({ error: "Сервис маршрутов не настроен" }, { status: 503 });

  try {
    const response = await fetch(`${config.baseURL}/api/v1/telegram/commute`, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-API-Key": config.apiKey },
      body: JSON.stringify({ origin: body.origin, destination: body.destination, mode: "approximated_transit" }),
      cache: "no-store",
      signal: AbortSignal.timeout(15_000),
    });
    const result = await response.json().catch(() => null) as unknown;
    if (!response.ok) {
      const status = response.status === 429 ? 429 : response.status >= 500 ? 503 : 400;
      const message = status === 429 ? "Лимит расчётов достигнут. Попробуйте позже." : "Не удалось рассчитать маршрут";
      return Response.json({ error: message }, { status });
    }
    return Response.json(result, { headers: { "Cache-Control": "no-store" } });
  } catch {
    return Response.json({ error: "Сервис маршрутов временно недоступен" }, { status: 503 });
  }
}
