import type { BuildingInfo } from "../model/types";

export type BuildingRatingCriterion = {
  key: "safety" | "age" | "elevators" | "energy" | "walls";
  label: string;
  score: number;
  detail: string;
};

export type BuildingRating = {
  score: number;
  criteria: BuildingRatingCriterion[];
};

type WeightedCriterion = BuildingRatingCriterion & { weight: number };

function validYear(value: number | undefined, currentYear: number): value is number {
  return typeof value === "number" && Number.isInteger(value) && value >= 1700 && value <= currentYear + 1;
}

function ageCriterion(building: BuildingInfo, currentYear: number): WeightedCriterion | null {
  const year = validYear(building.built_year, currentYear)
    ? building.built_year
    : validYear(building.exploitation_start_year, currentYear)
      ? building.exploitation_start_year
      : null;
  if (year === null) return null;
  const age = Math.max(0, currentYear - year);
  const score = age <= 10 ? 10 : age <= 30 ? 8.5 : age <= 50 ? 7 : age <= 80 ? 5.5 : 4;
  return { key: "age", label: "Возраст", score, weight: 3, detail: String(year) + " год, около " + String(age) + " лет" };
}

function elevatorCriterion(building: BuildingInfo): WeightedCriterion | null {
  const floors = building.floor_count_max;
  const elevators = building.elevators_count;
  if (typeof floors !== "number" || floors <= 0 || typeof elevators !== "number" || elevators < 0) return null;
  let score = 9;
  if (floors > 5 && elevators === 0) score = 2;
  else if (floors >= 16 && elevators < 2) score = 5;
  else if (floors >= 10 && elevators === 1) score = 6.5;
  return {
    key: "elevators",
    label: "Лифты",
    score,
    weight: 2,
    detail: String(elevators) + " на дом при этажности до " + String(floors),
  };
}

function energyCriterion(building: BuildingInfo): WeightedCriterion | null {
  const raw = building.energy_efficiency?.trim().toUpperCase();
  if (!raw) return null;
  const letter = raw.replace(/[+\s]/g, "").charAt(0);
  const scores: Record<string, number> = { A: 10, А: 10, B: 9, В: 9, C: 8, С: 8, D: 6.5, Д: 6.5, E: 5, Е: 5, F: 3, G: 2 };
  const score = scores[letter];
  if (typeof score !== "number") return null;
  return { key: "energy", label: "Энергоэффективность", score, weight: 1.5, detail: "класс " + building.energy_efficiency };
}

function wallCriterion(building: BuildingInfo): WeightedCriterion | null {
  const material = building.wall_material?.trim();
  if (!material) return null;
  const value = material.toLowerCase();
  let score: number | null = null;
  if (/кирп|монолит/.test(value)) score = 9;
  else if (/панел/.test(value)) score = 6.5;
  else if (/блоч|газоб|пеноб/.test(value)) score = 6;
  else if (/дерев|бревн|брус/.test(value)) score = 3;
  if (score === null) return null;
  return { key: "walls", label: "Материал стен", score, weight: 1, detail: material };
}

export function getBuildingRating(building: BuildingInfo, currentYear = new Date().getFullYear()): BuildingRating | null {
  const criteria: WeightedCriterion[] = [];
  if (typeof building.is_alarm === "boolean") {
    criteria.push({
      key: "safety",
      label: "Аварийность",
      score: building.is_alarm ? 1 : 10,
      weight: 5,
      detail: building.is_alarm ? "дом отмечен как аварийный" : "дом не отмечен как аварийный",
    });
  }
  for (const criterion of [
    ageCriterion(building, currentYear),
    elevatorCriterion(building),
    energyCriterion(building),
    wallCriterion(building),
  ]) {
    if (criterion) criteria.push(criterion);
  }

  if (building.is_alarm !== true && criteria.length < 2) return null;
  const weight = criteria.reduce((sum, criterion) => sum + criterion.weight, 0);
  if (weight === 0) return null;
  let score = criteria.reduce((sum, criterion) => sum + criterion.score * criterion.weight, 0) / weight;
  if (building.is_alarm === true) score = Math.min(score, 1.5);
  return {
    score: Math.round(score * 10) / 10,
    criteria: criteria.map((criterion) => ({
      key: criterion.key,
      label: criterion.label,
      score: criterion.score,
      detail: criterion.detail,
    })),
  };
}
