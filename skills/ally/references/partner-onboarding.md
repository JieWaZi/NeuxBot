# Partner onboarding

Guide the user through setup in this chat. If Core has not supplied previously accepted interview answers, make `waker profile interview` your first user-facing action. In one card, ask two or three short questions in the user's language. Tailor the questions and their choices to the partner's overview. Cover core responsibilities, work style, and working methods without re-asking what is already known. Give each question two to five distinct, meaningful options that can be selected together. Core adds an Other option that accepts custom text alongside any selected choices. Do not add an Other option yourself or send the questions as chat text. Use `interaction questions wait` with the returned question ID to obtain the actual answers before saving the profile. Keep waiting if observation times out while the question is pending.

Once you have the accepted interview answers, generate the profile and immediately call `waker profile save` with the complete payload and a stable request ID. Use the exact execution context supplied by Core. If the result is uncertain, recover the receipt with the same request ID. After a successful receipt, tell the user the profile is ready and can be edited in the partner overview.

Write all five overview fields in the user's language, using the exact structures below. Use the user's initial overview and accepted answers as the source of truth.

- `roleName` is a short role title that identifies what the partner does, such as `Research analyst`. The user-provided partner name is saved separately.
- `bio` is a one-sentence introduction that refines the initial overview with the agreed responsibilities and deliverables. Generate it from the interview even when the initial overview is blank.
- `expertise` is an array of cards. Each card has a compact title pairing a responsibility with its outcome, such as `📚 Organize sources · Extract findings`, and one concrete sentence explaining scope and deliverable in `description`. Use a suitable emoji and a middle dot in the title.
- `workStyle` is an array of short, distinct labels that fit on chips, such as `Conclusion first`, `Clear and direct`, and `Proactive collaboration`. Use phrases without sentence-ending punctuation.
- `workMethods` is an array of named workflows. Each `name` identifies a typical task, such as `Source organization`. Each `steps` array lists short action phrases in execution order, such as `Confirm the purpose`, `Summarize key points`, `Check the sources`.

Write `content` as concise English Markdown for `AGENTS.md`. This text is the partner's standing prompt. Aim for 150 to 250 words and keep it within 1,300 cl100k_base tokens. Translate preferences into English. Keep only durable responsibilities, concrete collaboration or workflow rules, and authorization boundaries. Prefer a few short bullets over prose, repeated summaries, examples, and routine steps. If the save rejects the length, shorten the payload and retry.

## Command examples

Use the exact execution context supplied by Core. Tailor the interview to the overview before writing its UTF-8 payload. You can pass JSON on standard input with `--input -` when a file is unnecessary.

<!-- input: partner-interview.json -->
```json
{
  "message": "Let's agree on how we work together.",
  "questions": [
    {"id": "responsibilities", "title": "Which research work should I own?", "options": [
      {"id": "sources", "label": "Find and compare primary sources"},
      {"id": "review", "label": "Review drafts and flag unsupported claims"}
    ]},
    {"id": "workStyle", "title": "How should I report uncertainty?", "options": [
      {"id": "compact", "label": "Summarize uncertainty alongside each finding"},
      {"id": "separate", "label": "Collect open questions at the end"}
    ]},
    {"id": "workingMethods", "title": "How should I check conclusions?", "options": [
      {"id": "crosscheck", "label": "Cross-check against independent sources"},
      {"id": "reviewpoints", "label": "Share checkpoints for your review"}
    ]}
  ]
}
```

```sh
allycli waker profile interview --input partner-interview.json --request-id interview-01
allycli interaction questions wait --question-id interview-example --timeout 30s
```

After the user answers, save the UTF-8 profile payload. Use the user's language for the overview fields. This example uses English in both places.

<!-- input: partner-profile.json -->
```json
{
  "roleName": "Research analyst",
  "bio": "I compare primary sources and review drafts so you can act on concise, verifiable findings.",
  "expertise": [
    {"title": "📚 Organize sources · Extract findings", "description": "Compare primary material, extract findings, and identify open questions."},
    {"title": "📝 Review drafts · Verify claims", "description": "Check the evidence and reasoning in drafts and flag unsupported claims."}
  ],
  "workStyle": ["Conclusion first", "Surface uncertainty"],
  "workMethods": [
    {"name": "Source organization", "steps": ["Confirm the purpose", "Check the sources", "Deliver findings"]},
    {"name": "Draft review", "steps": ["Read the draft", "Verify the claims", "Flag open questions"]}
  ],
  "content": "# Research partner\n\n## Responsibilities\nOrganize source material and review drafts for unsupported claims.\n\n## Working agreement\nLead with the conclusion and mark uncertainty. Confirm the purpose, check the sources, and deliver concise findings.\n"
}
```

```sh
allycli waker profile save --input partner-profile.json --request-id save-01
```
