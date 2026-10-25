/**
 * The single source for the syntax color families the app's code surfaces
 * use: one-dark for the dark scheme, one-light for the light scheme. Two
 * lazy-chunk consumers assemble their theme objects from these constants —
 * the terminal's curated ANSI 16 (TerminalView.tsx) and CodeMirror's
 * highlight rules (cm-themes.ts) — so a family color is defined exactly
 * once and the two views can never drift apart.
 *
 * The palette holds only the syntax hues; surface colors (background,
 * foreground, caret, gutter) are theme-token driven and selection tints /
 * token-fallback colors stay with their consumers (they are not part of
 * the designed syntax families).
 *
 * Chunk boundary: this module is inlined into BOTH lazy chunks that import
 * it (terminal and editor) — the constants are plain strings, the per-chunk
 * copy is a few hundred bytes, and no cross-chunk import is introduced. The
 * core bundle does not (and must not) import this module.
 */

/** one-dark family (dark scheme) syntax hues. */
export const ONE_DARK = {
  /** Editor-background tone; ANSI black. */
  black: "var(--editor-syntax-black)",
  /** Default foreground gray; ANSI white. */
  gray: "var(--editor-syntax-gray)",
  /** Comment gray; ANSI bright black. */
  faintGray: "var(--editor-syntax-faint-gray)",
  /** Pure white; ANSI bright white and the invalid-token color. */
  white: "var(--editor-syntax-white)",
  red: "var(--editor-syntax-red)",
  green: "var(--editor-syntax-green)",
  yellow: "var(--editor-syntax-yellow)",
  blue: "var(--editor-syntax-blue)",
  magenta: "var(--editor-syntax-magenta)",
  cyan: "var(--editor-syntax-cyan)",
  /** Numbers, bools, atoms, attribute names. */
  orange: "var(--editor-syntax-orange)",
} as const;

/** one-light family (light scheme) syntax hues. */
export const ONE_LIGHT = {
  /** Foreground dark; ANSI black and the operator color. */
  black: "var(--editor-syntax-black)",
  /** Comment gray; ANSI white. */
  gray: "var(--editor-syntax-gray)",
  /** ANSI bright black. */
  faintGray: "var(--editor-syntax-faint-gray)",
  /** Pure white; the invalid-token color (stays white in light for contrast). */
  white: "var(--editor-syntax-white)",
  /** Off-white; ANSI bright white. */
  offWhite: "var(--editor-syntax-off-white)",
  red: "var(--editor-syntax-red)",
  green: "var(--editor-syntax-green)",
  yellow: "var(--editor-syntax-yellow)",
  blue: "var(--editor-syntax-blue)",
  magenta: "var(--editor-syntax-magenta)",
  cyan: "var(--editor-syntax-cyan)",
  /** Numbers and attribute names. */
  orange: "var(--editor-syntax-orange)",
  /** Markdown links — the light family's dedicated link blue (dark rides blue). */
  link: "var(--editor-syntax-link)",
} as const;
