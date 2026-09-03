interface ErrorBannerProps {
  message: string | null;
  onDismiss: () => void;
}

export function ErrorBanner({ message, onDismiss }: ErrorBannerProps) {
  if (!message) return null;

  return (
    <div className="error-banner">
      <span>{message}</span>
      <button onClick={onDismiss}>×</button>
    </div>
  );
}
