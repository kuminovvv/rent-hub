export function formatAnnouncementCount(count: number): string {
  const tens = count % 100;
  const ones = count % 10;
  const noun =
    tens >= 11 && tens <= 14
      ? "объявлений"
      : ones === 1
        ? "объявление"
        : ones >= 2 && ones <= 4
          ? "объявления"
          : "объявлений";

  return `${new Intl.NumberFormat("ru-RU").format(count)} ${noun}`;
}
