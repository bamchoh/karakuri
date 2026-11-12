type Props = {
  size?: number;
  className?: string;
};

export function KarakuriLogo({ size = 32, className }: Props) {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 200 200"
      width={size}
      height={size}
      className={className}
      aria-hidden="true"
    >
      {/* 外二重丸 */}
      <circle
        cx="100"
        cy="100"
        r="93"
        fill="none"
        stroke="currentColor"
        strokeWidth="6"
      />
      <circle
        cx="100"
        cy="100"
        r="85"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
      />

      {/* 八枚歯車（中心穴あり・evenodd） */}
      <path
        fillRule="evenodd"
        fill="currentColor"
        d="
          M  87.6, 46.4 L  89.1, 30.9 L 110.9, 30.9 L 112.4, 46.4
          L 129.1, 53.4 L 141.1, 43.4 L 156.6, 58.9 L 146.6, 70.9
          L 153.6, 87.6 L 169.1, 89.1 L 169.1,110.9 L 153.6,112.4
          L 146.6,129.1 L 156.6,141.1 L 141.1,156.6 L 129.1,146.6
          L 112.4,153.6 L 110.9,169.1 L  89.1,169.1 L  87.6,153.6
          L  70.9,146.6 L  58.9,156.6 L  43.4,141.1 L  53.4,129.1
          L  46.4,112.4 L  30.9,110.9 L  30.9, 89.1 L  46.4, 87.6
          L  53.4, 70.9 L  43.4, 58.9 L  58.9, 43.4 L  70.9, 53.4 Z
          M 138,100 A 38,38 0 1,0 62,100 A 38,38 0 1,0 138,100 Z
        "
      />

      {/* 中心ハブ */}
      <circle cx="100" cy="100" r="12" fill="currentColor" />
    </svg>
  );
}
