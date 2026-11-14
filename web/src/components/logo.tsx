// Marca de LCS: una "L" de escuadra con un punto de cota, sin fondo.
// El trazo usa currentColor (se adapta a claro/oscuro) y el punto el acento del tema.
// build/appicon*.svg y public/favicon.svg son el mismo dibujo.
export function Logo({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 256 256" className={className} aria-hidden="true" fill="none">
      <path d="M84 62v118a14 14 0 0 0 14 14h76" stroke="currentColor" strokeWidth="26" strokeLinecap="round" strokeLinejoin="round" />
      <circle cx="170" cy="86" r="17" fill="var(--primary)" />
    </svg>
  )
}
