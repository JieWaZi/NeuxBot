export class CommandLineArgumentsError extends Error {
  constructor() {
    super("Invalid command-line arguments");
    this.name = "CommandLineArgumentsError";
  }
}

export function parseCommandLineArguments(value: string): string[] {
  const argumentsList: string[] = [];
  let argument = "";
  let argumentStarted = false;
  let quote: '"' | "'" | undefined;

  const appendEscapedCharacter = (index: number) => {
    const nextCharacter = value[index + 1];
    if (
      nextCharacter &&
      (isWhitespace(nextCharacter) ||
        nextCharacter === '"' ||
        nextCharacter === "'" ||
        nextCharacter === "\\")
    ) {
      argument += nextCharacter;
      return index + 1;
    }
    argument += "\\";
    return index;
  };

  for (let index = 0; index < value.length; index += 1) {
    const character = value[index] ?? "";
    if (quote) {
      if (character === quote) {
        quote = undefined;
      } else if (character === "\\" && quote === '"') {
        index = appendEscapedCharacter(index);
      } else {
        argument += character;
      }
      argumentStarted = true;
      continue;
    }

    if (isWhitespace(character)) {
      if (argumentStarted) {
        argumentsList.push(argument);
        argument = "";
        argumentStarted = false;
      }
      continue;
    }
    if (character === '"' || character === "'") {
      quote = character;
      argumentStarted = true;
      continue;
    }
    if (character === "\\") {
      index = appendEscapedCharacter(index);
      argumentStarted = true;
      continue;
    }
    argument += character;
    argumentStarted = true;
  }

  if (quote) {
    throw new CommandLineArgumentsError();
  }
  if (argumentStarted) {
    argumentsList.push(argument);
  }
  return argumentsList;
}

export function formatCommandLineArguments(
  argumentsList: readonly string[],
): string {
  return argumentsList.map(formatArgument).join(" ");
}

function formatArgument(argument: string) {
  if (argument && !/[\s'"\\]/u.test(argument)) {
    return argument;
  }
  return `"${argument.replaceAll("\\", "\\\\").replaceAll('"', '\\"')}"`;
}

function isWhitespace(character: string) {
  return /\s/u.test(character);
}
