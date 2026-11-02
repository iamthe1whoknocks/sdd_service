// Client mirror of the payload rules in internal/validate/validate.go
// (docs/spec.md §4, FE-4). This module is the single source of truth for the
// web UI: components must not re-implement the limits inline (docs/FIXES.md #18).
//
// The measurements have to stay identical to the server, otherwise the client
// rejects payloads the server would accept (or the other way round):
//   - question and title are bounded in Unicode code points (runes), like
//     utf8.RuneCountInString on the server (docs/FIXES.md #8);
//   - content is bounded in BYTES, like len(string) on the server,
//     octet_length(content) in migrations and echo BodyLimit;
//   - length checks run on the TRIMMED value, because the handlers trim the
//     field before calling validate (internal/handler/ask.go).

// Limits from docs/spec.md §4 and api/openapi.yaml.
export const MAX_QUESTION = 4000
export const MAX_TITLE = 500
export const MAX_CONTENT_BYTES = 1 << 20 // 1 MB

// charLen counts Unicode code points, matching Go utf8.RuneCountInString.
// String.length would count UTF-16 code units and cut emoji/supplementary
// characters roughly in half, which is why maxLength attributes are not used.
export const charLen = (s: string): number => Array.from(s).length

// byteLen counts UTF-8 bytes, matching Go len(string) and SQL octet_length.
export const byteLen = (s: string): number => new TextEncoder().encode(s).length

// Each validator returns the message to show the user, or null when the value
// is valid. Messages are UI text, the rules are what must match the server.

export function validateQuestion(question: string): string | null {
  const trimmed = question.trim()
  if (charLen(trimmed) === 0) {
    return 'Вопрос обязателен'
  }
  if (charLen(trimmed) > MAX_QUESTION) {
    return `Вопрос не длиннее ${MAX_QUESTION} символов`
  }
  return null
}

export function validateTitle(title: string): string | null {
  const trimmed = title.trim()
  if (charLen(trimmed) === 0) {
    return 'Title обязателен'
  }
  if (charLen(trimmed) > MAX_TITLE) {
    return `Title не длиннее ${MAX_TITLE} символов`
  }
  return null
}

// validateContent keeps the server order: the byte limit is reported first
// (the handler maps it to 413), the missing-content check second (422).
// The byte size is measured on the raw value — trimming would understate it.
export function validateContent(content: string): string | null {
  const bytes = byteLen(content)
  if (bytes > MAX_CONTENT_BYTES) {
    return `Content не больше 1 МБ (сейчас ${(bytes / MAX_CONTENT_BYTES).toFixed(2)} МБ)`
  }
  if (content.trim() === '') {
    return 'Content обязателен'
  }
  return null
}
