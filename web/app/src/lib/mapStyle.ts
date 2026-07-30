export const DEFAULT_ROUTE_COLOR = '#c2451e'

export interface HighwayStyle {
  color: string
  weight: number
}

export const HIGHWAY_STYLES: Record<string, HighwayStyle> = {
  motorway: { color: '#e11d48', weight: 5 },
  trunk: { color: '#f97316', weight: 4.5 },
  primary: { color: '#f59e0b', weight: 4 },
  secondary: { color: '#fbbf24', weight: 3.5 },
  tertiary: { color: '#fde047', weight: 3 },
  residential: { color: '#94a3b8', weight: 2 },
  service: { color: '#64748b', weight: 1.5 },
  unclassified: { color: '#94a3b8', weight: 2 },
  living_street: { color: '#a3a3a3', weight: 2 },
  pedestrian: { color: '#0e7490', weight: 2 },
  footway: { color: '#0e7490', weight: 1.5 },
  path: { color: '#0e7490', weight: 1.5 },
  cycleway: { color: '#34d399', weight: 2 },
  steps: { color: '#a78bfa', weight: 1.5 },
}

export const DEFAULT_HIGHWAY_STYLE: HighwayStyle = { color: '#475569', weight: 1.2 }

export const highwayStyle = (highway?: string): HighwayStyle =>
  (highway && HIGHWAY_STYLES[highway]) || DEFAULT_HIGHWAY_STYLE

/** Stable display order for the street-type legend. */
export const HIGHWAY_LEGEND_ORDER = Object.keys(HIGHWAY_STYLES)
