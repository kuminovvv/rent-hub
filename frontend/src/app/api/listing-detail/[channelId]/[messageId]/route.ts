import { getBuildingForPost, getPostById, ListingApiError } from "@/entities/listing";

type RouteContext = {
  params: Promise<{ channelId: string; messageId: string }>;
};

function positiveInteger(value: string): number | null {
  if (!/^\d+$/.test(value)) return null;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : null;
}

function channelIdentifier(value: string): number | null {
  if (!/^-?\d+$/.test(value)) return null;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed !== 0 ? parsed : null;
}

export async function GET(_request: Request, { params }: RouteContext) {
  const { channelId: rawChannelId, messageId: rawMessageId } = await params;
  const channelId = channelIdentifier(rawChannelId);
  const messageId = positiveInteger(rawMessageId);
  if (channelId === null || messageId === null) {
    return Response.json({ error: "Некорректный идентификатор объявления" }, { status: 400 });
  }

  try {
    const [post, building] = await Promise.all([
      getPostById(channelId, messageId),
      getBuildingForPost(channelId, messageId).catch(() => null),
    ]);
    if (!post) return Response.json({ error: "Объявление не найдено" }, { status: 404 });
    return Response.json(building ? { ...post, building } : post, { headers: { "Cache-Control": "no-store" } });
  } catch (error) {
    const status = error instanceof ListingApiError && error.kind === "http" ? 502 : 503;
    return Response.json({ error: "Не удалось загрузить объявление" }, { status, headers: { "Cache-Control": "no-store" } });
  }
}
