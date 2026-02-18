import type { CCUsageGroup } from '../api'

function barColor(percentage: number): string {
  if (percentage >= 86) return '#ef4444'
  if (percentage >= 60) return '#f59e0b'
  return '#22c55e'
}

export function CCUsageBar({
  label,
  percentage,
  resetDuration,
}: {
  label: string
  percentage: number
  resetDuration: string
}) {
  const color = barColor(percentage)

  return (
    <div style={{ marginBottom: '8px' }}>
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          marginBottom: '4px',
          fontSize: '13px',
        }}
      >
        <span style={{ fontWeight: 500, color: '#374151' }}>{label}</span>
        <span style={{ color: '#6b7280', fontSize: '12px' }}>resets in {resetDuration}</span>
      </div>
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '8px',
        }}
      >
        <div
          style={{
            flex: 1,
            height: '8px',
            backgroundColor: '#f3f4f6',
            borderRadius: '4px',
            overflow: 'hidden',
          }}
        >
          <div
            data-testid="usage-bar-fill"
            style={{
              width: `${percentage}%`,
              height: '100%',
              backgroundColor: color,
              borderRadius: '4px',
              transition: 'width 0.3s ease',
            }}
          />
        </div>
        <span style={{ fontSize: '13px', fontWeight: 600, color, minWidth: '36px', textAlign: 'right' }}>
          {percentage}%
        </span>
      </div>
    </div>
  )
}

export function CCUsageSection({ groups }: { groups: CCUsageGroup[] }) {
  return (
    <div>
      {groups.map((group) => (
        <div key={group.group_label} style={{ marginBottom: '16px' }}>
          <h4
            style={{
              margin: '0 0 8px 0',
              fontSize: '13px',
              fontWeight: 600,
              color: '#6b7280',
              textTransform: 'uppercase',
              letterSpacing: '0.5px',
            }}
          >
            {group.group_label}
          </h4>
          {group.lines.map((line) => (
            <CCUsageBar
              key={line.label}
              label={line.label}
              percentage={line.percentage}
              resetDuration={line.reset_duration}
            />
          ))}
        </div>
      ))}
    </div>
  )
}
