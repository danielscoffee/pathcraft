import type { ComponentType } from 'react'
import {
  AirIcon,
  BikeIcon,
  BusIcon,
  CarIcon,
  RouteIcon,
  WalkIcon,
  type IconProps,
} from '../components/Icons'

/** Manifest icon key → visual component; unknown plugins get a generic route icon. */
const MODE_ICONS: Record<string, ComponentType<IconProps>> = {
  air: AirIcon,
  bike: BikeIcon,
  bus: BusIcon,
  car: CarIcon,
  walk: WalkIcon,
}

export const modeIcon = (key?: string): ComponentType<IconProps> =>
  (key && MODE_ICONS[key]) || RouteIcon
