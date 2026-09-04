interface Props {
  /** Used for the accessible name, so each button is distinguishable. */
  label: string
  onClick: () => void
}

/**
 * Opens a paper in a background tab.
 *
 * A sibling of the card rather than a child: the card is itself a `<button>`,
 * and nesting one inside another is invalid HTML and does not reliably click.
 * The caller positions it over the card's top-right corner.
 */
export default function OpenInTabButton({ label, onClick }: Props) {
  return (
    <button
      type="button"
      className="open-in-tab"
      aria-label={`Open ${label} in a background tab`}
      title="Open in a background tab"
      onClick={onClick}
    >
      <svg
        viewBox="0 0 16 16"
        width="13"
        height="13"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
        focusable="false"
      >
        {/* Square, open at the top-right where the arrow leaves it. */}
        <path d="M9 3.5H4.5A1.5 1.5 0 0 0 3 5v6.5A1.5 1.5 0 0 0 4.5 13H11a1.5 1.5 0 0 0 1.5-1.5V7" />
        {/* Arrow, tip past the square's top-right corner. */}
        <path d="M7.6 8.4 14.5 1.5" />
        <path d="M10.4 1.5h4.1v4.1" />
      </svg>
    </button>
  )
}
