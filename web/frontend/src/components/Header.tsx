import { ThemeToggle } from './ThemeToggle';

export function Header() {
  return (
    <header>
      <div className="header-text">
        <h1>Ticket Live Event Scanner</h1>
        <p>Event Discovery &amp; Notification Dashboard</p>
      </div>
      <ThemeToggle />
    </header>
  );
}
