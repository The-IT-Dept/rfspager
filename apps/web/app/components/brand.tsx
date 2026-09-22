/**
 * The pager mark from the mobile app icon, drawn in `currentColor` so it can sit
 * on the white header and the near-black footer alike. The screen, buttons and
 * base bar are real holes punched with a mask rather than white fills, which is
 * what lets the background show through on dark.
 */
export function BrandMark({ className = "" }: { className?: string }) {
  return (
    <svg
      viewBox="82 146 908 664"
      fill="currentColor"
      aria-hidden="true"
      className={className}
    >
      <mask id="rfs-pager-cutouts">
        <rect x="112" y="426" width="620" height="380" rx="108" fill="#fff" />
        <rect x="180" y="500" width="372" height="140" rx="38" fill="#000" />
        <rect x="592" y="514" width="82" height="56" rx="28" fill="#000" />
        <rect x="592" y="600" width="82" height="56" rx="28" fill="#000" />
        <rect x="180" y="684" width="372" height="50" rx="25" fill="#000" />
        <circle cx="234" cy="570" r="16" fill="#fff" />
        <rect x="270" y="557" width="150" height="26" rx="13" fill="#fff" />
      </mask>
      <g fill="none" stroke="currentColor" strokeLinecap="round" strokeWidth="60">
        <path d="M690 306 a 140 140 0 0 1 140 140" opacity=".95" />
        <path d="M690 176 a 270 270 0 0 1 270 270" opacity=".5" />
      </g>
      <rect
        x="112"
        y="426"
        width="620"
        height="380"
        rx="108"
        mask="url(#rfs-pager-cutouts)"
      />
    </svg>
  );
}

/**
 * Mark plus wordmark. The wordmark stays an SVG of the original brand
 * letterforms — it is not Montserrat, so setting it as live text would quietly
 * redraw the logo.
 */
export function BrandLockup({
  className = "",
  markClassName = "h-8 w-auto text-[#ff1003]",
  wordmarkClassName = "h-5 w-auto",
}: {
  className?: string;
  markClassName?: string;
  wordmarkClassName?: string;
}) {
  return (
    <span className={`inline-flex items-center gap-2 ${className}`}>
      <BrandMark className={markClassName} />
      <img src="/logo-wordmark.svg" alt="RFSPager" className={wordmarkClassName} />
    </span>
  );
}
