import { FormEvent, useEffect, useRef, useState } from 'react'
import {
  byteLen,
  charLen,
  MAX_CONTENT_BYTES,
  MAX_QUESTION,
  MAX_TITLE,
  validateContent,
  validateQuestion,
  validateTitle,
} from './validation'

type Source = { title: string; url?: string | null; snippet: string }
type AskResponse = { answer: string; sources: Source[] }
type DocumentResponse = { document_id: string; indexed: boolean }

async function postJSON<T>(url: string, body: unknown, signal?: AbortSignal): Promise<T> {
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    signal,
  })
  const data = await res.json().catch(() => null)
  if (!res.ok) {
    const detail = data?.error?.detail ?? `HTTP ${res.status}`
    throw new Error(detail)
  }
  return data as T
}

// fetch rejects with a DOMException named "AbortError" when the signal fires.
// That is a cancelled request, not an API failure: it must not be shown as an
// error nor overwrite the result of the previous successful call.
function isAbortError(err: unknown): boolean {
  return (err as { name?: string } | null)?.name === 'AbortError'
}

// The API contract has no stable identifier for a source (Source is
// title/url/snippet, see api/openapi.yaml), so the React key is built from the
// fields themselves. The counter disambiguates exact duplicates, which do
// happen: several chunks of one document share title and snippet prefix.
function buildSourceKeys(sources: Source[]): string[] {
  const seen = new Map<string, number>()
  return sources.map((s) => {
    const base = `${s.title ?? ''}|${s.url ?? ''}|${(s.snippet ?? '').slice(0, 64)}`
    const repeat = seen.get(base) ?? 0
    seen.set(base, repeat + 1)
    return `${base}#${repeat}`
  })
}

type LoadState = { status: 'idle' } | { status: 'loading' } | { status: 'success' } | { status: 'error'; message: string }

function AskTab() {
  const [question, setQuestion] = useState('')
  const [state, setState] = useState<LoadState>({ status: 'idle' })
  const [response, setResponse] = useState<AskResponse | null>(null)
  const abortRef = useRef<AbortController | null>(null)

  // Cancel an in-flight request when the component unmounts (e.g. switching
  // tabs destroys AskTab while /ask may still run for up to 60 s).
  useEffect(() => () => abortRef.current?.abort(), [])

  const clientError = validateQuestion(question)
  const sourceKeys = response ? buildSourceKeys(response.sources) : []

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    if (clientError) return

    // A repeated submit must not leave the previous request running.
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller

    setState({ status: 'loading' })
    try {
      const data = await postJSON<AskResponse>('/api/v1/ask', { question: question.trim() }, controller.signal)
      setResponse(data)
      setState({ status: 'success' })
    } catch (err) {
      if (isAbortError(err)) return
      setState({ status: 'error', message: err instanceof Error ? err.message : String(err) })
    } finally {
      if (abortRef.current === controller) abortRef.current = null
    }
  }

  return (
    <section>
      <form onSubmit={onSubmit}>
        <label htmlFor="question">Запрос об уязвимости</label>
        <textarea
          id="question"
          value={question}
          onChange={(e) => setQuestion(e.target.value)}
          placeholder="Например: что такое Log4Shell?"
        />
        <div className="counter">
          {charLen(question)}/{MAX_QUESTION}
        </div>
        <button className="primary" type="submit" disabled={state.status === 'loading' || !!clientError}>
          {state.status === 'loading' ? 'Генерация…' : 'Запросить'}
        </button>
      </form>

      {clientError && <div className="error">{clientError}</div>}
      {state.status === 'error' && <div className="error">{state.message}</div>}

      {response && (
        <div className="result">
          <strong>Ответ:</strong>
          <p>{response.answer}</p>
          {response.sources.length > 0 && (
            <>
              <strong>Источники ({response.sources.length}):</strong>
              {response.sources.map((s, i) => (
                <div className="source" key={sourceKeys[i]}>
                  <div>
                    <b>{s.title}</b>
                    {s.url && (
                      <span className="meta">
                        {' '}
                        — <a href={s.url}>{s.url}</a>
                      </span>
                    )}
                  </div>
                  <div className="meta">{s.snippet}</div>
                </div>
              ))}
            </>
          )}
        </div>
      )}
    </section>
  )
}

function DocumentsTab() {
  const [title, setTitle] = useState('')
  const [content, setContent] = useState('')
  const [state, setState] = useState<LoadState>({ status: 'idle' })
  const [result, setResult] = useState<DocumentResponse | null>(null)
  const abortRef = useRef<AbortController | null>(null)

  // Same as AskTab: indexing talks to Ollama and PostgreSQL, so it can outlive
  // the component when the operator switches tabs.
  useEffect(() => () => abortRef.current?.abort(), [])

  const contentBytes = byteLen(content)
  const clientError = validateTitle(title) ?? validateContent(content)

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    if (clientError) return

    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller

    setState({ status: 'loading' })
    try {
      const data = await postJSON<DocumentResponse>(
        '/api/v1/documents',
        {
          title: title.trim(),
          content,
        },
        controller.signal,
      )
      setResult(data)
      setState({ status: 'success' })
    } catch (err) {
      if (isAbortError(err)) return
      setState({ status: 'error', message: err instanceof Error ? err.message : String(err) })
    } finally {
      if (abortRef.current === controller) abortRef.current = null
    }
  }

  return (
    <section>
      <form onSubmit={onSubmit}>
        <label htmlFor="title">Title</label>
        <input
          id="title"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="CVE-2021-44228"
        />
        <div className="counter">
          {charLen(title)}/{MAX_TITLE}
        </div>

        <label htmlFor="content">Content</label>
        <textarea
          id="content"
          value={content}
          onChange={(e) => setContent(e.target.value)}
          placeholder="Описание уязвимости…"
        />
        <div className="counter">
          {(contentBytes / MAX_CONTENT_BYTES).toFixed(3)} МБ / 1 МБ
        </div>

        <button className="primary" type="submit" disabled={state.status === 'loading' || !!clientError}>
          {state.status === 'loading' ? 'Индексация…' : 'Сохранить документ'}
        </button>
      </form>

      {clientError && <div className="error">{clientError}</div>}
      {state.status === 'error' && <div className="error">{state.message}</div>}

      {result && (
        <div className="result">
          <strong>document_id:</strong> <code>{result.document_id}</code>
          <div className="meta">indexed: {String(result.indexed)}</div>
        </div>
      )}
    </section>
  )
}

export default function App() {
  const [tab, setTab] = useState<'ask' | 'documents'>('ask')

  return (
    <main>
      <h1>VulnRAG — ассистент по открытым источникам об уязвимостях</h1>
      <nav>
        <button className={tab === 'ask' ? 'active' : ''} onClick={() => setTab('ask')}>
          Запрос
        </button>
        <button className={tab === 'documents' ? 'active' : ''} onClick={() => setTab('documents')}>
          Сохранение документа
        </button>
      </nav>
      {tab === 'ask' ? <AskTab /> : <DocumentsTab />}
    </main>
  )
}