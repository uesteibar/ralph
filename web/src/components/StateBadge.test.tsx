import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { StateBadge, STATE_COLORS } from './StateBadge'

describe('STATE_COLORS', () => {
  it('maps all expected states to color strings', () => {
    const expectedStates = [
      'queued', 'refining', 'approved', 'building',
      'in_review', 'addressing_feedback', 'completed',
      'failed', 'paused',
    ]
    for (const state of expectedStates) {
      expect(STATE_COLORS[state]).toMatch(/^#[0-9a-f]{6}$/i)
    }
  })
})

describe('StateBadge', () => {
  it('renders the state text with underscores replaced by spaces', () => {
    render(<StateBadge state="in_review" />)
    expect(screen.getByText('in review')).toBeInTheDocument()
  })

  it('renders a simple state without underscores', () => {
    render(<StateBadge state="building" />)
    expect(screen.getByText('building')).toBeInTheDocument()
  })

  it('applies the matching background color from STATE_COLORS', () => {
    render(<StateBadge state="completed" />)
    const badge = screen.getByText('completed')
    expect(badge).toHaveStyle({ backgroundColor: STATE_COLORS.completed })
  })

  it('falls back to default color for unknown states', () => {
    render(<StateBadge state="unknown_state" />)
    const badge = screen.getByText('unknown state')
    expect(badge).toHaveStyle({ backgroundColor: '#6b7280' })
  })
})
