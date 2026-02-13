// eslint-disable-next-line react-refresh/only-export-components
export const STATE_COLORS: Record<string, string> = {
  queued: '#6b7280',
  refining: '#8b5cf6',
  approved: '#3b82f6',
  building: '#f59e0b',
  in_review: '#10b981',
  addressing_feedback: '#ef4444',
  completed: '#22c55e',
  failed: '#dc2626',
  paused: '#9ca3af',
}

export function StateBadge({ state }: { state: string }) {
  const color = STATE_COLORS[state] || '#6b7280'
  return (
    <span
      style={{
        display: 'inline-block',
        padding: '2px 8px',
        borderRadius: '12px',
        fontSize: '12px',
        fontWeight: 600,
        color: '#fff',
        backgroundColor: color,
        textTransform: 'uppercase',
        letterSpacing: '0.5px',
      }}
    >
      {state.replace(/_/g, ' ')}
    </span>
  )
}
