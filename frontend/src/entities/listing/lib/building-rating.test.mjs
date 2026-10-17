import assert from "node:assert/strict";
import test from "node:test";

import { getBuildingRating } from "./building-rating.ts";

function building(overrides = {}) {
  return {
    source: "frt",
    house_guid: "test-house",
    address: "Тестовая улица, 1",
    imported_at: "2026-10-11T00:00:00Z",
    ...overrides,
  };
}

test("returns null when rating fields are unknown", () => {
  assert.equal(getBuildingRating(building(), 2026), null);
});

test("caps an alarm building at 1.5 even with otherwise good fields", () => {
  const rating = getBuildingRating(building({
    is_alarm: true,
    built_year: 2024,
    floor_count_max: 12,
    elevators_count: 3,
    energy_efficiency: "A",
    wall_material: "Монолит",
  }), 2026);

  assert.ok(rating);
  assert.ok(rating.score <= 1.5);
});

test("no elevators in a high-rise lowers the rating", () => {
  const base = {
    is_alarm: false,
    built_year: 2020,
    floor_count_max: 16,
    energy_efficiency: "A",
    wall_material: "Монолит",
  };

  const withoutElevators = getBuildingRating(building({ ...base, elevators_count: 0 }), 2026);
  const withElevators = getBuildingRating(building({ ...base, elevators_count: 3 }), 2026);

  assert.ok(withoutElevators);
  assert.ok(withElevators);
  assert.ok(withoutElevators.score < withElevators.score);
});

test("good confirmed fields produce a rating above 8", () => {
  const rating = getBuildingRating(building({
    is_alarm: false,
    built_year: 2020,
    floor_count_max: 16,
    elevators_count: 3,
    energy_efficiency: "A",
    wall_material: "Монолит",
  }), 2026);

  assert.ok(rating);
  assert.ok(rating.score > 8);
});
