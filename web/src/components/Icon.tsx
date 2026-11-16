import type { ReactNode } from 'react'

// 24px 网格、1.75px 描边的线性图标
const paths: Record<string, ReactNode> = {
  home: <path d="M4 10.5 12 4l8 6.5V19a1 1 0 0 1-1 1h-4.5v-5.5h-5V20H5a1 1 0 0 1-1-1z" />,
  music: (
    <>
      <path d="M9 17.5V6l10-2v11.5" />
      <circle cx="6.5" cy="17.5" r="2.5" />
      <circle cx="16.5" cy="15.5" r="2.5" />
    </>
  ),
  search: (
    <>
      <circle cx="11" cy="11" r="6.5" />
      <path d="m16 16 4 4" />
    </>
  ),
  sliders: (
    <>
      <path d="M4 7h9M17 7h3M4 17h3M11 17h9" />
      <circle cx="15" cy="7" r="2" />
      <circle cx="9" cy="17" r="2" />
    </>
  ),
  chevron: <path d="m9 6 6 6-6 6" />,
  sort: <path d="M7 5v14m0 0-3-3m3 3 3-3M14 7h6M14 12h4.5M14 17h3" />,
  close: <path d="m7 7 10 10M17 7 7 17" />,
  check: <path d="m5 12.5 4.5 4.5L19 7.5" />,
  alert: (
    <>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M12 8v4.5M12 16h.01" />
    </>
  ),
  chart: <path d="M4 19h16M7 16v-5M12 16V6M17 16v-8" />,
  database: (
    <>
      <ellipse cx="12" cy="6.5" rx="7" ry="2.75" />
      <path d="M5 6.5v11c0 1.5 3.1 2.75 7 2.75s7-1.25 7-2.75v-11M5 12c0 1.5 3.1 2.75 7 2.75s7-1.25 7-2.75" />
    </>
  ),
  key: (
    <>
      <circle cx="8" cy="15" r="4" />
      <path d="m11 12 8.5-8.5M16 7l2.5 2.5" />
    </>
  ),
  puzzle: (
    <path d="M9.5 4.5h5v3a1.75 1.75 0 1 0 3.5 0v0h1.5V13h-2a1.75 1.75 0 1 0 0 3.5h2v3h-5.25v-1.75a1.75 1.75 0 1 0-3.5 0v1.75H4.5v-5.25h1.75a1.75 1.75 0 1 0 0-3.5H4.5V7.5h5z" />
  ),
  file: (
    <>
      <path d="M7 3.5h7l4 4V20a.5.5 0 0 1-.5.5h-10A.5.5 0 0 1 7 20z" transform="translate(-.5)" />
      <path d="M9 12h6M9 16h4" />
    </>
  ),
  terminal: (
    <>
      <rect x="3.5" y="5" width="17" height="14" rx="2" />
      <path d="m7.5 10 2.5 2-2.5 2M12.5 14.5h4" />
    </>
  ),
  refresh: <path d="M19.5 12a7.5 7.5 0 1 1-2.2-5.3M19.5 4.5v3.5H16" />,
  trash: <path d="M5 7h14M10 7V4.5h4V7M7 7l.75 12a1 1 0 0 0 1 .95h6.5a1 1 0 0 0 1-.95L17 7M10.5 11v5M13.5 11v5" />,
  external: <path d="M14 5h5v5M19 5l-8 8M10 6H6a1 1 0 0 0-1 1v11a1 1 0 0 0 1 1h11a1 1 0 0 0 1-1v-4" />,
  send: <path d="m20 4-8.5 16-2-7-6-2.5z M9.5 13 20 4" />,
}

export type IconName = keyof typeof paths

export function Icon({ name, size = 24 }: { name: IconName; size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.75}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
    >
      {paths[name]}
    </svg>
  )
}
