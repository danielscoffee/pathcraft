import type { SVGProps } from 'react'

type IconProps = SVGProps<SVGSVGElement> & { size?: number }

function base({ size = 18, ...rest }: IconProps): SVGProps<SVGSVGElement> {
  return {
    width: size,
    height: size,
    viewBox: '0 0 24 24',
    fill: 'none',
    stroke: 'currentColor',
    strokeWidth: 1.8,
    strokeLinecap: 'round',
    strokeLinejoin: 'round',
    'aria-hidden': true,
    ...rest,
  }
}

export const WalkIcon = (p: IconProps) => (
  <svg {...base(p)}>
    <circle cx="13" cy="4.5" r="2" fill="currentColor" stroke="none" />
    <path d="M10 20.5l2-5-2.5-3 1-4.5 3.5 1 2.5 3M9.5 12.5L7 14.5M13 15l2.5 5.5" />
  </svg>
)

export const BusIcon = (p: IconProps) => (
  <svg {...base(p)}>
    <rect x="4.5" y="3.5" width="15" height="14" rx="2.5" />
    <path d="M4.5 11.5h15M8 21l1-3.5M16 21l-1-3.5" />
    <circle cx="8.5" cy="14.7" r="0.9" fill="currentColor" stroke="none" />
    <circle cx="15.5" cy="14.7" r="0.9" fill="currentColor" stroke="none" />
  </svg>
)

export const CarIcon = (p: IconProps) => (
  <svg {...base(p)}>
    <path d="M4 16v2.5M20 16v2.5M3.5 11l1.7-4.6A2 2 0 017.1 5h9.8a2 2 0 011.9 1.4L20.5 11M3.5 11h17a1 1 0 011 1v3a1 1 0 01-1 1h-17a1 1 0 01-1-1v-3a1 1 0 011-1z" />
    <circle cx="7.5" cy="13.5" r="1" fill="currentColor" stroke="none" />
    <circle cx="16.5" cy="13.5" r="1" fill="currentColor" stroke="none" />
  </svg>
)

export const BikeIcon = (p: IconProps) => (
  <svg {...base(p)}>
    <circle cx="6" cy="16" r="3.5" />
    <circle cx="18" cy="16" r="3.5" />
    <path d="M6 16l3.5-6.5h5L18 16M9.5 9.5H8M14 7h2.5l1 2.5" />
    <circle cx="14.7" cy="5.2" r="1.4" fill="currentColor" stroke="none" />
  </svg>
)

export const SwapIcon = (p: IconProps) => (
  <svg {...base(p)}>
    <path d="M7 4v13M7 4L4 7M7 4l3 3M17 20V7M17 20l-3-3M17 20l3-3" />
  </svg>
)

export const LayersIcon = (p: IconProps) => (
  <svg {...base(p)}>
    <path d="M12 3l9 5-9 5-9-5 9-5z" />
    <path d="M3.5 12.5L12 17l8.5-4.5M3.5 16.5L12 21l8.5-4.5" />
  </svg>
)

export const ClearIcon = (p: IconProps) => (
  <svg {...base(p)}>
    <path d="M6 6l12 12M18 6L6 18" />
  </svg>
)

export type { IconProps }
