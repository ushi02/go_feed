import { useEffect, useRef, useState } from 'react'
import { ApiError, apiError, errorText, type Messages } from './messages'

type Card = { feed_url: string; title: string; site_url: string; subscribed: boolean; reason: string }
// open: an answer part still receiving text; a "break" event closes it.
type Item = { id?: number; kind: 'user' | 'assistant' | 'card'; text?: string; card?: Card; incomplete?: boolean; open?: boolean }

async function api(path: string, init: RequestInit = {}): Promise<Response> {
  const res = await fetch(`/api${path}`, { credentials: 'include', headers: { 'Content-Type': 'application/json' }, ...init })
  if (!res.ok) throw await apiError(res)
  return res
}

// readEvents parses server-sent events from a fetch response. EventSource
// cannot be used: it only sends GET, and a message is a POST.
async function readEvents(body: ReadableStream<Uint8Array>, on: (event: string, data: any) => void) {
  const reader = body.getReader()
  const decoder = new TextDecoder()
  let buf = ''
  for (;;) {
    const { done, value } = await reader.read()
    if (done) return
    buf += decoder.decode(value, { stream: true })
    let end
    while ((end = buf.indexOf('\n\n')) >= 0) {
      const block = buf.slice(0, end)
      buf = buf.slice(end + 2)
      let event = 'message', data = ''
      for (const line of block.split('\n')) {
        if (line.startsWith('event: ')) event = line.slice(7)
        else if (line.startsWith('data: ')) data += line.slice(6)
      }
      if (data) on(event, JSON.parse(data))
    }
  }
}

function appendText(items: Item[], piece: string): Item[] {
  const last = items[items.length - 1]
  if (last?.kind === 'assistant' && last.open) return [...items.slice(0, -1), { ...last, text: last.text + piece }]
  return [...items, { kind: 'assistant', text: piece, open: true }]
}

const closeLast = (items: Item[]): Item[] => items.map((x, i) => i === items.length - 1 && x.open ? { ...x, open: false } : x)

const inlinePattern = /(\*\*[^*]+\*\*|`[^`]+`|\[[^\]]+\]\(https?:\/\/[^)\s]+\)|https?:\/\/[^\s<>"'()[\]{}，。、；：！？（）【】《》「」]+)/

function inline(text: string) {
  return text.split(inlinePattern).map((part, i) => {
    if (i % 2 === 0) return part
    if (part.startsWith('**')) return <strong key={i}>{part.slice(2, -2)}</strong>
    if (part.startsWith('`')) return <code key={i}>{part.slice(1, -1)}</code>
    const md = part.match(/^\[([^\]]+)\]\((.+)\)$/)
    return <a key={i} href={md ? md[2] : part} target="_blank" rel="noopener noreferrer">{md ? md[1] : part}</a>
  })
}

// Markdown renders the little Markdown Gugu writes: paragraphs, headings,
// lists, **bold**, `code` and links. It builds React elements, never an HTML
// string, so model output cannot inject markup (no dangerouslySetInnerHTML).
// Links are already checked by the server; only http(s) ones match here.
function Markdown({ text }: { text: string }) {
  const blocks: React.ReactNode[] = []
  let list: { ordered: boolean; items: string[] } | null = null
  const flush = () => {
    const l = list
    if (!l) return
    const items = l.items.map((t, i) => <li key={i}>{inline(t)}</li>)
    blocks.push(l.ordered ? <ol key={blocks.length}>{items}</ol> : <ul key={blocks.length}>{items}</ul>)
    list = null
  }
  for (const line of text.split('\n')) {
    const m = line.match(/^\s*(?:[-*]|(\d+)[.)])\s+(.*)$/)
    if (m) {
      const ordered = m[1] !== undefined
      if (!list || list.ordered !== ordered) { flush(); list = { ordered, items: [] } }
      list.items.push(m[2])
      continue
    }
    flush()
    const heading = line.match(/^#{1,6}\s+(.*)$/)
    if (heading) blocks.push(<p key={blocks.length}><strong>{inline(heading[1])}</strong></p>)
    else if (line.trim()) blocks.push(<p key={blocks.length}>{inline(line)}</p>)
  }
  flush()
  return <>{blocks}</>
}

// Pigeon is a placeholder until the 3D model is chosen.
export function Pigeon({ size = 56 }: { size?: number }) {
  return <svg className="pigeon" width={size} height={size} viewBox="0 0 64 64" aria-hidden="true">
    <defs><linearGradient id="gugu-neck" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stopColor="#5fb59a" /><stop offset="1" stopColor="#8a6fb3" /></linearGradient></defs>
    <path d="M50 40 L61 34 L59 44 Z" fill="#8d99a2" />
    <ellipse cx="34" cy="40" rx="20" ry="15" fill="#c3ccd1" />
    <ellipse cx="39" cy="41" rx="11" ry="8" fill="#a3aeb5" />
    <path d="M33 41 q6 3 13 0 M34 45 q6 3 12 0" stroke="#75828b" strokeWidth="1.6" fill="none" strokeLinecap="round" />
    <ellipse cx="24" cy="31" rx="10" ry="9" fill="url(#gugu-neck)" />
    <circle cx="22" cy="22" r="10" fill="#97a3ab" />
    <circle cx="19" cy="20" r="2.6" fill="#1d2a30" /><circle cx="18.2" cy="19.2" r=".9" fill="#fff" />
    <circle cx="24.5" cy="25" r="2.2" fill="#f2a7a7" opacity=".75" />
    <path d="M12.5 21.5 L6.5 24 L12.5 25.5 Z" fill="#f0a35e" />
    <path d="M29 54 v4 M27 58 h4 M37 54 v4 M35 58 h4" stroke="#e98a6f" strokeWidth="1.8" strokeLinecap="round" />
  </svg>
}

export default function Chat({ t, subscribedURLs, onSubscribe }: { t: Messages; subscribedURLs: Set<string>; onSubscribe: (url: string) => Promise<boolean> }) {
  const [open, setOpen] = useState(false)
  const [items, setItems] = useState<Item[] | null>(null)
  const [input, setInput] = useState('')
  const [sending, setSending] = useState(false)
  const [error, setError] = useState('')
  const bodyRef = useRef<HTMLDivElement>(null)

  const load = () => api('/chat').then(r => r.json() as Promise<{ items: Item[] }>).then(d => setItems(d.items)).catch(e => setError(errorText(t, e)))
  useEffect(() => { if (open && items === null) load() }, [open]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { bodyRef.current?.scrollTo({ top: bodyRef.current.scrollHeight }) }, [items, open])

  async function send(text: string) {
    text = text.trim()
    if (!text || sending) return
    setInput(''); setError(''); setSending(true)
    setItems(it => [...(it || []), { kind: 'user', text }])
    try {
      const res = await api('/chat/messages', { method: 'POST', body: JSON.stringify({ text }) })
      await readEvents(res.body!, (event, data) => {
        if (event === 'text') setItems(it => appendText(it || [], data.text))
        else if (event === 'break') setItems(it => closeLast(it || []))
        else if (event === 'card') setItems(it => [...closeLast(it || []), { kind: 'card', card: data }])
        else if (event === 'error') { setItems(it => (it || []).map((x, i, a) => i === a.length - 1 && x.kind === 'assistant' ? { ...x, incomplete: true } : x)); setError(errorText(t, new ApiError(0, data.code || '', data.error || ''))) }
      })
      setItems(it => closeLast(it || []))
    } catch (e) {
      // Reload from the server: it knows whether the message was stored
      // (a refused message, e.g. over the daily limit, is not).
      setError(errorText(t, e))
      load()
    } finally { setSending(false) }
  }

  async function newConversation() {
    if (sending) return
    try { await api('/chat/conversations', { method: 'POST' }); setItems([]); setError('') } catch (e) { setError(errorText(t, e)) }
  }

  function onKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    // While an input method is composing (e.g. picking Chinese characters),
    // Enter confirms the characters; it must not send the message.
    if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing && e.keyCode !== 229) {
      e.preventDefault(); send(input)
    }
  }

  // Show the dots whenever Gugu is working but no text is arriving, e.g.
  // while it checks blogs between two parts of the answer.
  const last = items?.[items.length - 1]
  const waiting = sending && !(last?.kind === 'assistant' && last.open)
  return <>
    <button className={`gugu-button ${open ? 'hidden' : ''}`} onClick={() => setOpen(true)} title={t.guguOpenTitle} aria-label={t.guguOpen}><Pigeon /></button>
    <aside className={`gugu-panel ${open ? 'open' : ''}`} aria-hidden={!open}>
      <header className="gugu-head">
        <Pigeon size={40} />
        <div><strong>{t.guguName}</strong><small>{t.guguTagline}</small></div>
        <button onClick={newConversation} disabled={sending} title={t.newChatTitle}>{t.newChat}</button>
        <button className="gugu-close" onClick={() => setOpen(false)} aria-label={t.close}>×</button>
      </header>
      <div className="gugu-body" ref={bodyRef}>
        {items?.length === 0 && <div className="gugu-empty">
          <Pigeon size={72} />
          <p>{t.guguHello}<br />{t.guguAsk}</p>
          <div className="gugu-suggest">{t.suggestions.map(s => <button key={s} onClick={() => send(s)}>{s}</button>)}</div>
        </div>}
        {items?.map((x, i) => {
          if (x.kind === 'user') return <div key={x.id ?? `u${i}`} className="bubble user">{x.text}</div>
          if (x.kind === 'assistant') return <div key={x.id ?? `a${i}`} className={`bubble gugu ${x.incomplete ? 'incomplete' : ''}`}>
            <Markdown text={x.text || ''} />{x.incomplete && <small className="gugu-cut">{t.replyCut}</small>}</div>
          const c = x.card!
          const done = c.subscribed || subscribedURLs.has(c.feed_url)
          let host = ''
          try { host = new URL(c.site_url || c.feed_url).host } catch { /* keep empty */ }
          return <div key={x.id ?? `c${i}`} className="gugu-card">
            <div className="gugu-card-top"><strong>{c.title || host}</strong>{host && <a href={c.site_url || c.feed_url} target="_blank" rel="noopener noreferrer">{host} ↗</a>}</div>
            {c.reason && <p>{c.reason}</p>}
            {done ? <span className="gugu-subscribed">{t.subscribedCheck}</span> : <button className="primary" onClick={() => onSubscribe(c.feed_url)}>{t.subscribe}</button>}
          </div>
        })}
        {waiting && <div className="bubble gugu typing"><span /><span /><span /></div>}
      </div>
      {error && <div className="gugu-error">{error}<button onClick={() => setError('')} aria-label={t.close}>×</button></div>}
      <form className="gugu-input" onSubmit={e => { e.preventDefault(); send(input) }}>
        <textarea value={input} onChange={e => setInput(e.target.value)} onKeyDown={onKeyDown} placeholder={t.chatPlaceholder} rows={2} maxLength={1000} disabled={sending} />
        <button className="primary" disabled={sending || !input.trim()}>{t.send}</button>
      </form>
    </aside>
  </>
}
