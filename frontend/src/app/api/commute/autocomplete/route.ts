type AutocompleteRequest = { query?: unknown };

function upstreamConfig() {
  const baseURL = (process.env.API_URL ?? "http://127.0.0.1:8082").replace(/\/+$/, "");
  const apiKey = process.env.TELEGRAM_API_KEY;
  if (!apiKey) return null;
  return { baseURL, apiKey };
}

export async function POST(request: Request) {
  let body: AutocompleteRequest;
  try {
    body = await request.json() as AutocompleteRequest;
  } catch {
    return Response.json({ error: "Некорректный запрос" }, { status: 400 });
  }

  if (typeof body.query !== "string" || body.query.trim().length < 4 || body.query.trim().length > 120) {
    return Response.json({ error: "Введите не менее четырёх символов" }, { status: 400 });
  }

  const config = upstreamConfig();
  if (!config) return Response.json({ error: "Сервис маршрутов не настроен" }, { status: 503 });

  try {
    const response = await fetch(`${config.baseURL}/api/v1/telegram/commute/autocomplete`, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-API-Key": config.apiKey },
      body: JSON.stringify({ query: body.query.trim() }),
      cache: "no-store",
      signal: AbortSignal.timeout(8_000),
    });
    const result = await response.json().catch(() => null) as unknown;
    if (!response.ok) {
      if (response.status === 429) return Response.json({ error: "Лимит подсказок исчерпан. Попробуйте позже." }, { status: 429 });
      return Response.json({ error: "Не удалось загрузить подсказки адресов" }, { status: 502 });
    }
    return Response.json(result, { headers: { "Cache-Control": "no-store" } });
  } catch {
    return Response.json({ error: "Подсказки адресов временно недоступны" }, { status: 503 });
  }
}
