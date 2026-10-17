import { Button } from "@/shared/ui/button";
import Link from "next/link";

export function SiteHeader() {
  return (
    <header className="site-header">
      <Link className="brand-mark" href="/" aria-label="Rent Hub — главная">
        RENT HUB
      </Link>
      <nav className="rental-mode" aria-label="Срок аренды">
        <Button className="rental-mode__active" aria-current="page">
          Снять на долго
        </Button>
        <Button
          className="rental-mode__inactive"
          disabled
          title="Посуточные объявления пока не подключены"
        >
          Посуточно
        </Button>
      </nav>
      <div aria-hidden="true" className="site-header__spacer" />
    </header>
  );
}
