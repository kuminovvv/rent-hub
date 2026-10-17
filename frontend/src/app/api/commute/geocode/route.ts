type GeocodeRequest = { address?: unknown };

function upstreamConfig() {
  const baseURL = (process.env.API_URL ?? "http://127.0.0.1:8082").replace(/\/+$/, "");
  const apiKey = process.env.TELEGRAM_API_KEY;
  if (!apiKey) return null;
  return { baseURL, apiKey };
}

export async function POST(request: Request) {
  let body: GeocodeRequest;
  try {
    body = await request.json() as GeocodeRequest;
  } catch {
    return Response.json({ error: "Некорректный запрос" }, { status: 400 });
  }

  if (typeof body.address !== "string" || body.address.trim().length < 5 || body.address.trim().length > 250) {
    return Response.json({ error: "Введите адрес длиной от 5 до 250 символов" }, { status: 400 });
  }

  const config = upstreamConfig();
  if (!config) return Response.json({ error: "Сервис маршрутов не настроен" }, { status: 503 });

  try {
    const response = await fetch(`${config.baseURL}/api/v1/telegram/commute/geocode`, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-API-Key": config.apiKey },
      body: JSON.stringify({ address: body.address.trim() }),
      cache: "no-store",
      signal: AbortSignal.timeout(15_000),
    });
    const result = await response.json().catch(() => null) as unknown;
    if (!response.ok) {
      if (response.status === 429) {
        return Response.json({ error: "Лимит запросов к Geoapify исчерпан" }, { status: 429 });
      }
      const message = response.status === 404 ? "Не удалось найти точный адрес в Санкт-Петербурге" : "Не удалось определить адрес";
      return Response.json({ error: message }, { status: response.status === 404 ? 404 : 502 });
    }
    return Response.json(result, { headers: { "Cache-Control": "no-store" } });
  } catch {
    return Response.json({ error: "Сервис маршрутов временно недоступен" }, { status: 503 });
  }
}
