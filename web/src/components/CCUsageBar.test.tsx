import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { CCUsageBar, CCUsageSection } from './CCUsageBar'
import type { CCUsageGroup } from '../api'

describe('CCUsageBar', () => {
  it('renders label and percentage', () => {
    render(<CCUsageBar label="5-hour" percentage={50} resetDuration="3h 13m" />)
    expect(screen.getByText('5-hour')).toBeInTheDocument()
    expect(screen.getByText('50%')).toBeInTheDocument()
  })

  it('renders reset duration', () => {
    render(<CCUsageBar label="7-day" percentage={30} resetDuration="2d 5h" />)
    expect(screen.getByText('resets in 2d 5h')).toBeInTheDocument()
  })

  it('applies green color when percentage is below 60', () => {
    const { container } = render(
      <CCUsageBar label="low" percentage={30} resetDuration="1h" />,
    )
    const bar = container.querySelector('[data-testid="usage-bar-fill"]')
    expect(bar).toHaveStyle({ backgroundColor: '#22c55e' })
  })

  it('applies amber color when percentage is between 60 and 85', () => {
    const { container } = render(
      <CCUsageBar label="mid" percentage={70} resetDuration="1h" />,
    )
    const bar = container.querySelector('[data-testid="usage-bar-fill"]')
    expect(bar).toHaveStyle({ backgroundColor: '#f59e0b' })
  })

  it('applies red color when percentage is above 85', () => {
    const { container } = render(
      <CCUsageBar label="high" percentage={90} resetDuration="1h" />,
    )
    const bar = container.querySelector('[data-testid="usage-bar-fill"]')
    expect(bar).toHaveStyle({ backgroundColor: '#ef4444' })
  })

  it('applies green at exactly 59%', () => {
    const { container } = render(
      <CCUsageBar label="edge" percentage={59} resetDuration="1h" />,
    )
    const bar = container.querySelector('[data-testid="usage-bar-fill"]')
    expect(bar).toHaveStyle({ backgroundColor: '#22c55e' })
  })

  it('applies amber at exactly 60%', () => {
    const { container } = render(
      <CCUsageBar label="edge" percentage={60} resetDuration="1h" />,
    )
    const bar = container.querySelector('[data-testid="usage-bar-fill"]')
    expect(bar).toHaveStyle({ backgroundColor: '#f59e0b' })
  })

  it('applies amber at exactly 85%', () => {
    const { container } = render(
      <CCUsageBar label="edge" percentage={85} resetDuration="1h" />,
    )
    const bar = container.querySelector('[data-testid="usage-bar-fill"]')
    expect(bar).toHaveStyle({ backgroundColor: '#f59e0b' })
  })

  it('applies red at exactly 86%', () => {
    const { container } = render(
      <CCUsageBar label="edge" percentage={86} resetDuration="1h" />,
    )
    const bar = container.querySelector('[data-testid="usage-bar-fill"]')
    expect(bar).toHaveStyle({ backgroundColor: '#ef4444' })
  })

  it('sets bar width to the percentage', () => {
    const { container } = render(
      <CCUsageBar label="test" percentage={42} resetDuration="1h" />,
    )
    const bar = container.querySelector('[data-testid="usage-bar-fill"]')
    expect(bar).toHaveStyle({ width: '42%' })
  })
})

describe('CCUsageSection', () => {
  it('renders group labels as section headers', () => {
    const groups: CCUsageGroup[] = [
      {
        group_label: 'Claude Code Usage Statistics',
        lines: [{ label: '5-hour', percentage: 50, reset_duration: '3h' }],
      },
    ]
    render(<CCUsageSection groups={groups} />)
    expect(screen.getByText('Claude Code Usage Statistics')).toBeInTheDocument()
  })

  it('renders multiple groups with their lines', () => {
    const groups: CCUsageGroup[] = [
      {
        group_label: 'Group A',
        lines: [
          { label: '5-hour', percentage: 30, reset_duration: '2h' },
          { label: '7-day', percentage: 80, reset_duration: '3d' },
        ],
      },
      {
        group_label: 'Group B',
        lines: [{ label: 'daily', percentage: 95, reset_duration: '1h' }],
      },
    ]
    render(<CCUsageSection groups={groups} />)
    expect(screen.getByText('Group A')).toBeInTheDocument()
    expect(screen.getByText('Group B')).toBeInTheDocument()
    expect(screen.getByText('5-hour')).toBeInTheDocument()
    expect(screen.getByText('7-day')).toBeInTheDocument()
    expect(screen.getByText('daily')).toBeInTheDocument()
    expect(screen.getByText('30%')).toBeInTheDocument()
    expect(screen.getByText('80%')).toBeInTheDocument()
    expect(screen.getByText('95%')).toBeInTheDocument()
  })
})
