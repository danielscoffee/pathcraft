import type { ComponentType } from 'react'
import { BikeIcon, BusIcon, CarIcon, WalkIcon, type IconProps } from '../components/Icons'

/** Mode id → icon component; unknown ids fall back to walking. */
const MODE_ICONS: Record<string, ComponentType<IconProps>> = {
  walk: WalkIcon,
  bus: BusIcon,
  car: CarIcon,
  bike: BikeIcon,
}

export const modeIcon = (id: string): ComponentType<IconProps> => MODE_ICONS[id] ?? WalkIcon
