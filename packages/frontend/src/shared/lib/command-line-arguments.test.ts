import { describe, expect, it } from "vitest";

import {
  CommandLineArgumentsError,
  formatCommandLineArguments,
  parseCommandLineArguments,
} from "./command-line-arguments";

describe("command-line arguments", () => {
  it("parses space-separated arguments pasted on one line", () => {
    expect(parseCommandLineArguments("-y chrome-devtools-mcp@latest")).toEqual([
      "-y",
      "chrome-devtools-mcp@latest",
    ]);
  });

  it("supports quotes, escaped whitespace, empty values and legacy newlines", () => {
    expect(
      parseCommandLineArguments(
        '--label "hello world" \'single value\' escaped\\ value ""\n--verbose',
      ),
    ).toEqual([
      "--label",
      "hello world",
      "single value",
      "escaped value",
      "",
      "--verbose",
    ]);
  });

  it("formats stored arrays as editable one-line input without losing values", () => {
    const argumentsList = [
      "-y",
      "hello world",
      'quote"value',
      "",
      "C:\\tools\\cli",
    ];

    expect(
      parseCommandLineArguments(formatCommandLineArguments(argumentsList)),
    ).toEqual(argumentsList);
  });

  it("rejects an unclosed quote", () => {
    expect(() => parseCommandLineArguments('--label "hello')).toThrow(
      CommandLineArgumentsError,
    );
  });
});
