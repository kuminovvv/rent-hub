import { Search } from "lucide-react";
import type { TelegramSource } from "@/entities/listing";
import { Button } from "@/shared/ui/button";
import { Input } from "@/shared/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/shared/ui/select";

type ListingSearchProps = {
  query: string;
  source: string;
  sources: TelegramSource[];
};

export function ListingSearch({ query, source, sources }: ListingSearchProps) {
  return (
    <div className="search-row">
      <label className="search-field">
        <Search aria-hidden="true" />
        <Input
          aria-label="Поиск по тексту объявлений"
          className="search-field__input"
          defaultValue={query}
          form="listing-search"
          name="q"
          placeholder="Район, улица или текст объявления"
        />
      </label>
      <label className="source-field">
        <span className="source-field__label">Источник</span>
        <Select
          defaultValue={source || null}
          form="listing-search"
          items={sources.map((item) => ({
            label: item.title || `@${item.username}`,
            value: item.username,
          }))}
          name="source"
        >
          <SelectTrigger aria-label="Источник объявлений" className="source-select">
            <SelectValue placeholder="Все каналы" />
          </SelectTrigger>
          <SelectContent>
            {sources.map((item) => (
              <SelectItem key={item.username} value={item.username}>
                {item.title || `@${item.username}`}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </label>
      <Button className="search-row__submit" form="listing-search" type="submit" variant="outline">
        Найти
      </Button>
    </div>
  );
}
