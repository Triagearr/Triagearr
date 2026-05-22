export function Logo({ size = 32 }: { size?: number }) {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 256 256"
      fill="none"
      role="img"
      aria-label="Triagearr"
      width={size}
      height={size}
    >
      <title>Triagearr</title>
      <polygon points="36,40 220,40 198,82 58,82" fill="#B85551" />
      <polygon points="64,94 192,94 170,138 86,138" fill="#C0913C" />
      <polygon points="94,150 162,150 128,214" fill="#4A9962" />
      <circle cx="128" cy="100" r="20" fill="#EFE6D0" stroke="#2A2D42" strokeWidth="1.25" />
      <path d="M 113,100 A 15,15 0 0 1 143,100" stroke="#9C8E6E" strokeWidth="3" fill="none" strokeLinecap="butt" opacity="0.35" />
      <path d="M 113,100 A 15,15 0 0 1 120.5,87.01" stroke="#4A9962" strokeWidth="3" fill="none" strokeLinecap="butt" />
      <path d="M 120.5,87.01 A 15,15 0 0 1 135.5,87.01" stroke="#C0913C" strokeWidth="3" fill="none" strokeLinecap="butt" />
      <path d="M 135.5,87.01 A 15,15 0 0 1 138.61,89.39" stroke="#B85551" strokeWidth="3" fill="none" strokeLinecap="butt" />
      <line x1="111.5" y1="100" x2="110" y2="100" stroke="#2A2D42" strokeWidth="1" strokeLinecap="butt" opacity="0.6" />
      <line x1="128" y1="83.5" x2="128" y2="82" stroke="#2A2D42" strokeWidth="1" strokeLinecap="butt" opacity="0.6" />
      <line x1="144.5" y1="100" x2="146" y2="100" stroke="#2A2D42" strokeWidth="1" strokeLinecap="butt" opacity="0.6" />
      <line x1="128" y1="100" x2="138.61" y2="89.39" stroke="#2A2D42" strokeWidth="2" strokeLinecap="round" />
      <circle cx="128" cy="100" r="2" fill="#2A2D42" />
    </svg>
  );
}
