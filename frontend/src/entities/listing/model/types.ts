export type TelegramPhoto = {
  kind: "photo";
  // Telegram IDs exceed JavaScript's safe integer range; use `url` for the exact ID.
  photo_id: number;
  url: string;
  width: number;
  height: number;
  bytes: number;
};

export type TelegramPost = {
  channel_id: number;
  message_id: number;
  source_username: string;
  source_title: string;
  source_url: string;
  text: string;
  media?: TelegramPhoto | TelegramPhoto[];
  posted_at: string;
  facts?: ListingFacts;
  building?: BuildingInfo;
};

export type BuildingInfo = {
  source: "frt";
  source_house_id?: number;
  house_guid: string;
  address: string;
  built_year?: number;
  exploitation_start_year?: number;
  project_type?: string;
  house_type?: string;
  is_alarm?: boolean;
  floor_count_max?: number;
  floor_count_min?: number;
  entrance_count?: number;
  elevators_count?: number;
  energy_efficiency?: string;
  quarters_count?: number;
  living_quarters_count?: number;
  foundation_type?: string;
  floor_type?: string;
  wall_material?: string;
  gas_type?: string;
  source_dataset_date?: string;
  imported_at: string;
};

export type ListingFacts = {
  kind?: string;
  building_type?: "new" | "secondary" | string | null;
  rooms?: number | null;
  area_sqm?: number | null;
  price_rub?: number | null;
  metro?: string | null;
  address?: string | null;
  latitude?: number | null;
  longitude?: number | null;
  details?: ListingDetailsFacts | null;
};

export type ListingDetailsFacts = {
  floor?: number | null;
  floors_total?: number | null;
  deposit_rub?: number | null;
  deposit_terms?: string | null;
  utilities_terms?: string | null;
  commission_terms?: string | null;
  lease_term?: string | null;
  tenant_requirements?: string | null;
  contact_name?: string | null;
  contact_phone?: string | null;
  contact_telegram?: string | null;
  contact_vk?: string | null;
  contact_methods?: Array<"phone" | "telegram" | "vk" | "other"> | null;
  features?: string[] | null;
};

export type ListingFilters = {
  q?: string;
  source?: string;
  buildingType?: "new" | "secondary";
  rooms?: Array<"0" | "1" | "2" | "3" | "4plus">;
  priceMin?: number;
  priceMax?: number;
  metro?: string;
};

export type MapListing = {
  channel_id: number;
  message_id: number;
  source_url: string;
  source_title: string;
  price_rub: number | null;
  rooms: number | null;
  building_type: string | null;
  address: string | null;
  latitude: number | null;
  longitude: number | null;
};

export type MapListingPage = {
  items: MapListing[];
  total: number;
  truncated?: boolean;
};

export type MapBounds = {
  west: number;
  south: number;
  east: number;
  north: number;
};

export type TelegramPostPage = {
  items: TelegramPost[];
  limit: number;
  has_more: boolean;
  next_cursor?: string;
  offset?: number;
};

export type TelegramSource = {
  channel_id: number;
  username: string;
  title: string;
  enabled: boolean;
};
