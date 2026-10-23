import { FormEvent, useState } from 'react'

// Доменные правила из docs/spec.md §4 дублируются клиентской валидацией (FE-4).
// charLen counts Unicode code points (matches Go utf8.RuneCountInString, docs/FIXES.md #8).
const charLen = (s: string) => Array.from(s).length
const byteLen = (s: string) => new TextEncoder().encode(s).length

const MAX_QUESTION = 4000
const MAX_TITLE = 500
const MAX_CONTENT = 1048576 // 1 МБ (bytes)

type Source = { title: string; url?: string | null; snippet: string }
type AskResponse = { answer: string; sources: Source[] }
type DocumentResponse = { document_id: string; indexed: boolean }

async function postJSON<T>(url: string, body: unknown): Promise<T> {
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  const data = await res.json().catch(() => null)
  if (!res.ok) {
    const detail = data?.error?.detail ?? `HTTP ${res.status}`
    throw new Error(detail)
  }
  return data as T
}

type LoadState = { status: 'idle' } | { status: 'loading' } | { status: 'success' } | { status: 'error'; message: string }

function AskTab() {
  const [question, setQuestion] = useState('')
  const [state, setState] = useState<LoadState>({ status: 'idle' })
  const [response, setResponse] = useState<AskResponse | null>(null)

  const clientError =
    charLen(question.trim()) === 0
      ? 'Вопрос обязателен'
      : charLen(question) > MAX_QUESTION
        ? `Вопрос не длиннее ${MAX_QUESTION} символов`
        : null

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    if (clientError) return
    setState({ status: 'loading' })
    try {
      const data = await postJSON<AskResponse>('/api/v1/ask', { question: question.trim() })
      setResponse(data)
      setState({ status: 'success' })
    } catch (err) {
      setState({ status: 'error', message: err instanceof Error ? err.message : String(err) })
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
          maxLength={MAX_QUESTION}
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
                <div className="source" key={i}>
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

  const contentBytes = byteLen(content)
  const clientError =
    charLen(title.trim()) === 0
      ? 'Title обязателен'
      : charLen(title) > MAX_TITLE
        ? `Title не длиннее ${MAX_TITLE} символов`
        : charLen(content.trim()) === 0
          ? 'Content обязателен'
          : contentBytes > MAX_CONTENT
            ? `Content не больше 1 МБ (сейчас ${(contentBytes / 1048576).toFixed(2)} МБ)`
            : null

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    if (clientError) return
    setState({ status: 'loading' })
    try {
      const data = await postJSON<DocumentResponse>('/api/v1/documents', {
        title: title.trim(),
        content,
      })
      setResult(data)
      setState({ status: 'success' })
    } catch (err) {
      setState({ status: 'error', message: err instanceof Error ? err.message : String(err) })
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
          maxLength={MAX_TITLE}
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
          {(contentBytes / 1048576).toFixed(3)} МБ / 1 МБ
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