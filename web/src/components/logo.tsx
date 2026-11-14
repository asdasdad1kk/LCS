// Marca de LCS. Es el mismo dibujo que build/appicon.svg (icono del .exe).
export function Logo({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 256 256" className={className} aria-hidden="true">
      <defs>
        <linearGradient id="lcs-logo-tile" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="#EC8F69" />
          <stop offset="1" stopColor="#C2553A" />
        </linearGradient>
        <linearGradient id="lcs-logo-sheen" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" stopColor="#fff" stopOpacity=".22" />
          <stop offset=".55" stopColor="#fff" stopOpacity="0" />
        </linearGradient>
      </defs>
      <rect x="12" y="12" width="232" height="232" rx="56" fill="url(#lcs-logo-tile)" />
      <rect x="12" y="12" width="232" height="232" rx="56" fill="url(#lcs-logo-sheen)" />
      <path
        d="M86 74v106h84"
        fill="none"
        stroke="#fff"
        strokeWidth="30"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <circle cx="163" cy="97" r="18" fill="#fff" />
    </svg>
  )
}
