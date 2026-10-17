const upstreamBaseUrl = () =>
  (process.env.API_URL ?? "http://127.0.0.1:8082").replace(/\/+$/, "");

export async function GET(
  request: Request,
  context: { params: Promise<{ photoId: string }> },
) {
  const { photoId } = await context.params;
  const normalizedPhotoId = photoId.replace(/^0+/, "");
  const exceedsInt64 =
    normalizedPhotoId.length > 19 ||
    (normalizedPhotoId.length === 19 &&
      normalizedPhotoId > "9223372036854775807");
  if (!/^\d+$/.test(photoId) || !normalizedPhotoId || exceedsInt64) {
    return new Response(null, { status: 404 });
  }

  const apiKey = process.env.TELEGRAM_API_KEY;
  if (!apiKey) {
    return new Response(null, { status: 503 });
  }

  let upstream: Response;
  try {
    const headers = new Headers({ "X-API-Key": apiKey });
    for (const name of ["if-none-match", "if-modified-since", "if-range", "range"]) {
      const value = request.headers.get(name);
      if (value) headers.set(name, value);
    }
    upstream = await fetch(
      `${upstreamBaseUrl()}/api/v1/telegram/media/${photoId}`,
      {
        headers,
        cache: "no-store",
      },
    );
  } catch {
    return new Response(null, { status: 502 });
  }

  const responseHeaders = new Headers({
    "Cache-Control": "private, max-age=31536000, immutable",
    "X-Content-Type-Options": "nosniff",
  });
  for (const name of ["etag", "last-modified", "accept-ranges", "content-range", "content-length"]) {
    const value = upstream.headers.get(name);
    if (value) responseHeaders.set(name, value);
  }

  if (upstream.status === 304) {
    return new Response(null, { status: 304, headers: responseHeaders });
  }

  if (!upstream.ok) {
    return new Response(null, {
      status: upstream.status === 404 ? 404 : 502,
      headers: { "Cache-Control": "private, no-store" },
    });
  }

  const contentType = upstream.headers.get("content-type")?.split(";", 1)[0];
  if (contentType !== "image/jpeg" || !upstream.body) {
    return new Response(null, {
      status: 502,
      headers: { "Cache-Control": "private, no-store" },
    });
  }

  responseHeaders.set("Content-Type", "image/jpeg");
  return new Response(upstream.body, {
    status: upstream.status,
    headers: responseHeaders,
  });
}
