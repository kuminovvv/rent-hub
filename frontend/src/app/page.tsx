import { HomePage } from "@/_pages/home";

const roomValues = new Set(["0", "1", "2", "3", "4plus"]);

export default async function Page({
  searchParams,
}: PageProps<"/">) {
  const params = await searchParams;
  const query = typeof params.q === "string" ? params.q : "";
  const source = typeof params.source === "string" ? params.source : "";
  const buildingType = params.building_type === "new" || params.building_type === "secondary" ? params.building_type : "";
  const roomParams = Array.isArray(params.rooms) ? params.rooms : params.rooms ? [params.rooms] : [];
  const rooms = Array.from(new Set(roomParams.filter((value) => roomValues.has(value))) as Set<"0" | "1" | "2" | "3" | "4plus">);
  const priceMin = typeof params.price_min === "string" && /^\d+$/.test(params.price_min) ? params.price_min : "";
  const priceMax = typeof params.price_max === "string" && /^\d+$/.test(params.price_max) ? params.price_max : "";
  const metro = typeof params.metro === "string" ? params.metro : "";
  const view = params.view === "map" ? "map" : "list";
  return <HomePage query={query} source={source} buildingType={buildingType} rooms={rooms} priceMin={priceMin} priceMax={priceMax} metro={metro} view={view} />;
}
