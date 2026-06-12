const EARTH_RADIUS_M = 6371000

export function haversineMeters(lat1: number, lon1: number, lat2: number, lon2: number): number {
  const toRad = (d: number) => (d * Math.PI) / 180
  const dLat = toRad(lat2 - lat1)
  const dLon = toRad(lon2 - lon1)
  const a =
    Math.sin(dLat / 2) ** 2 +
    Math.cos(toRad(lat1)) * Math.cos(toRad(lat2)) * Math.sin(dLon / 2) ** 2
  return 2 * EARTH_RADIUS_M * Math.asin(Math.sqrt(a))
}

/** Approximate length of a GeoJSON LineString coordinate array ([lon, lat] pairs). */
export function lineLengthMeters(coords: GeoJSON.Position[]): number {
  let total = 0
  for (let i = 1; i < coords.length; i++) {
    total += haversineMeters(coords[i - 1][1], coords[i - 1][0], coords[i][1], coords[i][0])
  }
  return total
}

const MODE_SPEED_MPS: Record<string, number> = { walk: 1.4, car: 8.3, bike: 4.5 }

export function estimateMinutes(modeID: string, distanceMeters: number): number {
  const speed = MODE_SPEED_MPS[modeID] ?? MODE_SPEED_MPS.walk
  return distanceMeters / speed / 60
}

/** "05:00" → "05:00:00"; values already carrying seconds pass through. */
export function normalizeClockTime(value: string): string {
  return /^\d{2}:\d{2}$/.test(value) ? `${value}:00` : value
}

export function formatNumber(n: number): string {
  return n.toLocaleString(undefined, { maximumFractionDigits: 1 })
}
