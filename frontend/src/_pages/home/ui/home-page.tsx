import {
  getPosts,
  getMapListings,
  getSources,
  type TelegramPost,
  type TelegramSource,
  type GetPostsOptions,
  type MapListing,
} from "@/entities/listing";
import { ListingFilters } from "@/widgets/listing-filters";
import { ListingResults } from "@/widgets/listing-results";
import { SiteHeader } from "@/widgets/site-header";

type HomePageProps = {
  query: string;
  source: string;
  buildingType: "new" | "secondary" | "";
  rooms: Array<"0" | "1" | "2" | "3" | "4plus">;
  priceMin: string;
  priceMax: string;
  metro: string;
  view: "list" | "map";
};

const PAGE_SIZE = 24;
const INITIAL_MAP_BOUNDS = { west: 29.3, south: 59.5, east: 31.3, north: 60.3 } as const;

type HomeData = {
  posts: TelegramPost[];
  hasMore: boolean;
  nextCursor?: string;
  sources: TelegramSource[];
  error: boolean;
  mapItems: MapListing[];
  mapError: boolean;
  mapTotal: number;
};

async function loadHomeData(filters: GetPostsOptions, view: "list" | "map"): Promise<HomeData> {
  const [postsResult, sourcesResult, mapResult] = await Promise.allSettled([
    view === "list"
      ? getPosts({ ...filters, hasText: true, offerOnly: true, limit: PAGE_SIZE })
      : Promise.resolve({ items: [], has_more: false, next_cursor: undefined }),
    getSources(),
    view === "map" ? getMapListings({ ...filters, bounds: INITIAL_MAP_BOUNDS, zoom: 11 }) : Promise.resolve({ items: [], total: 0 }),
  ]);
  const sources = sourcesResult.status === "fulfilled" ? sourcesResult.value : [];

  if (postsResult.status === "rejected") {
    return { posts: [], hasMore: false, nextCursor: undefined, sources, error: true, mapItems: [], mapError: mapResult.status === "rejected", mapTotal: 0 };
  }

  return {
    posts: postsResult.value.items,
    hasMore: postsResult.value.has_more,
    nextCursor: postsResult.value.next_cursor,
    sources,
    error: false,
    mapItems: mapResult.status === "fulfilled" ? mapResult.value.items : [],
    mapError: mapResult.status === "rejected",
    mapTotal: mapResult.status === "fulfilled" ? mapResult.value.total : 0,
  };
}

export async function HomePage({ query, source, buildingType, rooms, priceMin, priceMax, metro, view }: HomePageProps) {
  const filters: GetPostsOptions = {
    q: query,
    source,
    buildingType: buildingType || undefined,
    rooms: rooms.length ? rooms : undefined,
    priceMin: priceMin ? Number(priceMin) : undefined,
    priceMax: priceMax ? Number(priceMax) : undefined,
    metro,
  };
  const data = await loadHomeData(filters, view);

  return (
    <main className={`home-shell${view === "map" ? " home-shell--map" : ""}`}>
      <SiteHeader />
      <div className="home-content">
        <ListingFilters
          query={query}
          source={source}
          sources={data.sources}
          view={view}
          buildingType={buildingType}
          rooms={rooms}
          priceMin={priceMin}
          priceMax={priceMax}
          metro={metro}
        />
        <ListingResults
          key={JSON.stringify([query, source, buildingType, rooms, priceMin, priceMax, metro, view])}
          posts={data.posts}
          initialHasMore={data.hasMore}
          initialCursor={data.nextCursor}
          query={query}
          source={source}
          error={data.error}
          view={view}
          mapItems={data.mapItems}
          mapError={data.mapError}
          mapTotal={data.mapTotal}
          buildingType={buildingType}
          rooms={rooms}
          priceMin={priceMin}
          priceMax={priceMax}
          metro={metro}
        />
      </div>
    </main>
  );
}
