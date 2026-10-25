// 保留收藏卡的异形轮廓与层叠外沿，材质统一为实底表面，不绘制玻璃反光。
export function WakerShowcaseShell() {
  const body =
    "M28 55 Q28 29 55 29 H72 Q77 7 97 7 H158 Q178 7 183 29 L426 35 Q453 35 453 63 V109 Q453 120 446 127 V274 Q446 283 453 290 V471 Q453 483 447 490 V522 Q458 534 458 551 V623 Q458 651 431 651 H51 Q22 651 22 623 V553 Q22 544 28 538 Z";
  return (
    <svg
      className="ally-holo-card__shell"
      viewBox="0 0 480 680"
      preserveAspectRatio="none"
      aria-hidden="true"
    >
      <g fill="none" strokeLinejoin="round">
        <path
          d="M25 151 L18 164 L14 520 Q14 533 23 543 L15 554 L12 623 Q12 664 51 667 H386 L407 651 M459 125 L465 137 V259 L457 272 M458 526 L471 542 V625 Q471 667 434 667 H407 Q396 665 406 653"
          stroke="var(--surface-raised)"
          strokeWidth="10"
        />
        <path
          d="M25 151 L18 164 L14 520 Q14 533 23 543 L15 554 L12 623 Q12 664 51 667 H386 L407 651 M459 125 L465 137 V259 L457 272 M458 526 L471 542 V625 Q471 667 434 667 H407 Q396 665 406 653"
          stroke="var(--border-subtle)"
          strokeWidth="1"
        />
        <path
          d="M20 164 L17 520 M20 558 L17 623 Q17 662 51 662 H384 M466 551 V625 Q466 662 434 662"
          stroke="var(--border-subtle)"
          strokeWidth="1"
        />
      </g>
      <path
        d={body}
        fill="var(--surface-raised)"
        stroke="var(--border)"
        strokeWidth="1"
      />
      <path
        d="M35 162 L29 534 M31 553 L28 622 Q28 644 52 644 H425 Q450 644 450 621 V552"
        fill="none"
        stroke="var(--border-subtle)"
        strokeWidth="1"
      />
      <path
        d="M73 31 Q77 48 98 48 H160 Q178 48 182 33"
        fill="none"
        stroke="var(--border-subtle)"
        strokeWidth="1"
      />
    </svg>
  );
}
