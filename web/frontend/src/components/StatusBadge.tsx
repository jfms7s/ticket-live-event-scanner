interface StatusBadgeProps {
  status: string;
  type: string;
}

export function StatusBadge({ status, type }: StatusBadgeProps) {
  const label = status.charAt(0).toUpperCase() + status.slice(1);
  return <span className={`badge badge-${type}`}>{label}</span>;
}
