import { useCallback, useEffect, useMemo, useState } from 'react'
import Chat from './Chat'
import { apiError, errorText, initialLang, type Lang, langNames, langs, messages, saveLang } from './messages'

type User = { id: number; username: string }
type Folder = { id: number; name: string }
type Feed = { id: number; title: string; url: string; last_error: string; last_fetched_at: string | null; disabled_at: string | null }
type Subscription = { id: number; feed_id: number; folder_id: number | null; feed: Feed }
type Entry = { id: number; feed_id: number; feed_title: string; title: string; url: string; sort_at: string; read: boolean; starred_at: string | null }
type Candidate = { url: string; title: string }
type SearchResult = { entry_id: number; title: string; url: string; summary: string; sort_at: string; feed_id: number; feed_title: string; feed_url: string; subscribed: boolean }
type Filter = 'all' | 'unread' | 'starred'

// The GitHub mark from Octicons (MIT), inlined so no outside file is loaded.
const githubLink = <a className="github-link" href="https://github.com/ushi02/go_feed" target="_blank" rel="noopener noreferrer" aria-label="GitHub" title="GitHub">
  <svg viewBox="0 0 16 16" width="18" height="18" aria-hidden="true"><path fill="currentColor" d="M8 0c4.42 0 8 3.58 8 8a8.013 8.013 0 0 1-5.45 7.59c-.4.08-.55-.17-.55-.38 0-.27.01-1.13.01-2.2 0-.75-.25-1.23-.54-1.48 1.78-.2 3.65-.88 3.65-3.95 0-.88-.31-1.59-.82-2.15.08-.2.36-1.02-.08-2.12 0 0-.67-.22-2.2.82-.64-.18-1.32-.27-2-.27-.68 0-1.36.09-2 .27-1.53-1.03-2.2-.82-2.2-.82-.44 1.1-.16 1.92-.08 2.12-.51.56-.82 1.28-.82 2.15 0 3.06 1.86 3.75 3.64 3.95-.23.2-.44.55-.51 1.07-.46.21-1.61.55-2.33-.66-.15-.24-.6-.83-1.23-.82-.67.01-.27.38.01.53.34.19.73.9.82 1.13.16.45.68 1.31 2.69.94 0 .67.01 1.3.01 1.49 0 .21-.15.45-.55.38A7.995 7.995 0 0 1 0 8c0-4.42 3.58-8 8-8Z" /></svg>
</a>

// feedState changes whenever the Worker finishes a fetch, successful or not.
const feedState = (s: Subscription) => `${s.feed.last_fetched_at}|${s.feed.last_error}`

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const res = await fetch(`/api${path}`, {
    credentials: 'include',
    headers: init.body instanceof FormData ? undefined : { 'Content-Type': 'application/json' },
    ...init,
  })
  if (!res.ok) throw await apiError(res)
  // Some successful responses have no body (204, and 202 from refresh), so
  // parse JSON only when there is something to parse.
  const text = await res.text()
  return (text ? JSON.parse(text) : undefined) as T
}

// A panel that slides in from the right and leaves the same way. It plays the
// exit animation first and only then tells the parent to remove it.
const SHEET_EXIT_MS = 200
function Sheet({ onClose, className = '', children }: { onClose: () => void; className?: string; children: React.ReactNode }) {
  const [closing, setClosing] = useState(false)
  const close = () => { if (!closing) { setClosing(true); window.setTimeout(onClose, SHEET_EXIT_MS) } }
  return <div className={`reader-backdrop ${closing ? 'closing' : ''}`} onClick={close}>
    <section className={`reader ${className}`} onClick={e => e.stopPropagation()}><button className="close" onClick={close}>×</button>{children}</section>
  </div>
}

export default function App() {
  const [lang, setLang] = useState<Lang>(initialLang)
  const t = messages[lang]
  const [user, setUser] = useState<User | null>(null)
  const [ready, setReady] = useState(false)
  const [loginMode, setLoginMode] = useState<'login' | 'register'>('login')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [folders, setFolders] = useState<Folder[]>([])
  const [subs, setSubs] = useState<Subscription[]>([])
  const [entries, setEntries] = useState<Entry[]>([])
  const [cursor, setCursor] = useState('')
  const [filter, setFilter] = useState<Filter>('all')
  const [folderID, setFolderID] = useState<number | null>(null)
  const [subID, setSubID] = useState<number | null>(null)
  const [selected, setSelected] = useState<Entry | null>(null)
  const [body, setBody] = useState<{ id: number; content: string } | null>(null)
  const [unread, setUnread] = useState<Record<string, number>>({})
  // Subscriptions refreshed by hand: id → feed state at click time, and when to stop waiting.
  const [refreshing, setRefreshing] = useState<Record<number, { state: string; until: number }>>({})
  const [feedInput, setFeedInput] = useState('')
  const [newFolder, setNewFolder] = useState('')
  const [candidates, setCandidates] = useState<Candidate[]>([])
  const [addFolderID, setAddFolderID] = useState<number | null>(null)
  const [busy, setBusy] = useState(false)
  const [showManage, setShowManage] = useState(false)
  const [chatOpen, setChatOpen] = useState(false)
  const [searchInput, setSearchInput] = useState('')
  const [search, setSearch] = useState<{ q: string; results: SearchResult[] } | null>(null)

  const subscribedURLs = useMemo(() => new Set(subs.map(s => s.feed.url)), [subs])
  const report = (e: unknown) => { setError(errorText(t, e)); setNotice('') }
  const switchLang = (next: Lang) => { setLang(next); saveLang(next) }
  const langButton = <select className="lang-toggle" value={lang} onChange={e => switchLang(e.target.value as Lang)} aria-label={t.langLabel}>
    {langs.map(l => <option key={l} value={l}>{langNames[l]}</option>)}</select>
  const refreshNav = useCallback(async () => {
    const [f, s] = await Promise.all([
      request<{ folders: Folder[] }>('/folders'),
      request<{ subscriptions: Subscription[] }>('/subscriptions'),
    ])
    setFolders(f.folders || [])
    setSubs(s.subscriptions || [])
  }, [])
  const refreshUnread = useCallback(async () => {
    const data = await request<{ counts: Record<string, number> }>('/entries/unread-counts')
    setUnread(data.counts || {})
  }, [])
  const loadEntries = useCallback(async (next = '') => {
    const p = new URLSearchParams({ filter, limit: '20' })
    if (folderID !== null) p.set('folder_id', String(folderID))
    if (subID !== null) p.set('subscription_id', String(subID))
    if (next) p.set('cursor', next)
    const data = await request<{ entries: Entry[]; next_cursor: string }>(`/entries?${p}`)
    setEntries(old => next ? [...old, ...(data.entries || [])] : (data.entries || []))
    setCursor(data.next_cursor || '')
    if (!next) await refreshUnread()
  }, [filter, folderID, subID, refreshUnread])

  useEffect(() => { document.documentElement.lang = t.locale }, [t])
  useEffect(() => {
    request<User>('/me').then(setUser).catch(() => {}).finally(() => setReady(true))
  }, [])
  useEffect(() => {
    if (!user) return
    refreshNav().catch(report)
  }, [user, refreshNav])
  useEffect(() => {
    if (!user) return
    loadEntries().catch(report)
  }, [user, loadEntries])
  useEffect(() => {
    const done = Object.keys(refreshing).map(Number).filter(id => {
      const s = subs.find(x => x.id === id)
      return !s || feedState(s) !== refreshing[id].state || Date.now() > refreshing[id].until
    })
    if (done.length) setRefreshing(r => Object.fromEntries(Object.entries(r).filter(([id]) => !done.includes(Number(id)))))
  }, [subs, refreshing])
  useEffect(() => {
    const waiting = Object.keys(refreshing).length > 0 || subs.some(s => !s.feed.last_fetched_at && !s.feed.last_error)
    if (!user || !waiting) return
    const timer = window.setInterval(() => {
      Promise.all([refreshNav(), loadEntries()]).catch(report)
    }, 2500)
    return () => window.clearInterval(timer)
  }, [user, subs, refreshing, refreshNav, loadEntries])

  async function authenticate(e: React.FormEvent) {
    e.preventDefault(); setBusy(true); setError('')
    try {
      const u = await request<User>(`/auth/${loginMode}`, { method: 'POST', body: JSON.stringify({ username, password }) })
      setUser(u); setPassword('')
    } catch (e) { report(e) } finally { setBusy(false) }
  }
  async function logout() {
    try { await request('/auth/logout', { method: 'POST' }); setUser(null); setEntries([]); setSelected(null) } catch (e) { report(e) }
  }
  async function discover(e: React.FormEvent) {
    e.preventDefault(); setBusy(true); setError(''); setCandidates([])
    try {
      const input = /^https?:\/\//i.test(feedInput.trim()) ? feedInput.trim() : `https://${feedInput.trim()}`
      const data = await request<{ candidates: Candidate[] }>(`/discover?url=${encodeURIComponent(input)}`)
      if (data.candidates.length === 1) await addSubscription(data.candidates[0].url)
      else setCandidates(data.candidates)
    } catch (e) { report(e) } finally { setBusy(false) }
  }
  async function addSubscription(url: string) {
    try {
      await request('/subscriptions', { method: 'POST', body: JSON.stringify({ url, folder_id: addFolderID }) })
      setFeedInput(''); setCandidates([]); setNotice(t.subscribedNotice)
      await refreshNav(); await loadEntries()
      return true
    } catch (e) { report(e); return false }
  }
  async function runSearch(e: React.FormEvent) {
    e.preventDefault(); setBusy(true); setError('')
    const q = searchInput.trim()
    try {
      const data = await request<{ results: SearchResult[] }>(`/search?q=${encodeURIComponent(q)}`)
      setSearch({ q, results: data.results })
    } catch (e) { report(e) } finally { setBusy(false) }
  }
  async function subscribeFromSearch(r: SearchResult) {
    if (await addSubscription(r.feed_url)) setSearch(s => s && { ...s, results: s.results.map(x => x.feed_id === r.feed_id ? { ...x, subscribed: true } : x) })
  }
  async function createFolder(e: React.FormEvent) {
    e.preventDefault()
    try { await request('/folders', { method: 'POST', body: JSON.stringify({ name: newFolder }) }); setNewFolder(''); await refreshNav() } catch (e) { report(e) }
  }
  async function renameFolder(folder: Folder) {
    const name = window.prompt(t.folderNamePrompt, folder.name)?.trim()
    if (!name || name === folder.name) return
    try { await request(`/folders/${folder.id}`, { method: 'PATCH', body: JSON.stringify({ name }) }); await refreshNav() } catch (e) { report(e) }
  }
  async function deleteFolder(folder: Folder) {
    if (!window.confirm(t.confirmDeleteFolder(folder.name))) return
    try { await request(`/folders/${folder.id}`, { method: 'DELETE' }); if (folderID === folder.id) setFolderID(null); await refreshNav() } catch (e) { report(e) }
  }
  async function moveSubscription(sub: Subscription, target: number | null) {
    try { await request(`/subscriptions/${sub.id}`, { method: 'PATCH', body: JSON.stringify({ folder_id: target }) }); await refreshNav(); await loadEntries() } catch (e) { report(e) }
  }
  async function removeSubscription(id: number) {
    if (!window.confirm(t.confirmUnsubscribe)) return
    try { await request(`/subscriptions/${id}`, { method: 'DELETE' }); if (subID === id) setSubID(null); await refreshNav(); await loadEntries() } catch (e) { report(e) }
  }
  async function refreshSubscription(id: number) {
    const sub = subs.find(s => s.id === id)
    try {
      await request(`/subscriptions/${id}/refresh`, { method: 'POST' }); setNotice(t.refreshScheduled)
      if (sub) setRefreshing(r => ({ ...r, [id]: { state: feedState(sub), until: Date.now() + 60_000 } }))
    } catch (e) { report(e) }
  }
  async function openEntry(entry: Entry) {
    setSelected(entry)
    request<{ id: number; content: string }>(`/entries/${entry.id}`).then(d => setBody({ id: d.id, content: d.content })).catch(report)
    if (!entry.read) await changeState(entry, { read: true })
  }
  async function changeState(entry: Entry, patch: { read?: boolean; starred?: boolean }) {
    try {
      await request(`/entries/${entry.id}/state`, { method: 'PATCH', body: JSON.stringify(patch) })
      const updated = { ...entry, read: patch.read ?? entry.read, starred_at: patch.starred === undefined ? entry.starred_at : (patch.starred ? new Date().toISOString() : null) }
      setEntries(old => old.map(x => x.id === entry.id ? updated : x))
      setSelected(old => old?.id === entry.id ? updated : old)
      if (patch.read !== undefined) await refreshUnread()
    } catch (e) { report(e) }
  }
  async function markRead() {
    const scope = subID !== null ? { subscription_id: subID } : folderID !== null ? { folder_id: folderID } : {}
    try { await request('/entries/mark-read', { method: 'POST', body: JSON.stringify(scope) }); await loadEntries(); setNotice(t.markedRead) } catch (e) { report(e) }
  }
  async function importOPML(file: File) {
    const form = new FormData(); form.append('file', file)
    try {
      const data = await request<{ added: number; skipped: number }>('/opml/import', { method: 'POST', body: form })
      setNotice(t.imported(data.added, data.skipped))
      await refreshNav()
    } catch (e) { report(e) }
  }

  if (!ready) return <div className="loading">{t.loading}</div>
  if (!user) return <main className="auth-page"><section className="auth-card">
    <div className="auth-top"><span className="eyebrow">GUGU READER</span><span className="top-tools">{langButton}{githubLink}</span></div><h1>{t.authHeadline}</h1>
    <p>{t.authIntro}</p>
    <form onSubmit={authenticate}>
      <label>{t.username}<input value={username} onChange={e => setUsername(e.target.value)} minLength={3} required /></label>
      <label>{t.password}<input type="password" value={password} onChange={e => setPassword(e.target.value)} minLength={8} required /></label>
      <button className="primary" disabled={busy}>{loginMode === 'login' ? t.login : t.register}</button>
    </form>
    <button className="text-button" onClick={() => setLoginMode(loginMode === 'login' ? 'register' : 'login')}>{loginMode === 'login' ? t.toRegister : t.toLogin}</button>
    {error && <p className="error">{error}</p>}
  </section></main>

  const activeTitle = subID !== null ? subs.find(s => s.id === subID)?.feed.title || t.subscriptionFallback : folderID !== null ? folders.find(f => f.id === folderID)?.name || t.folders : t.allEntries
  const pendingCount = subs.filter(s => !s.feed.last_fetched_at && !s.feed.last_error).length
  // A failed feed is retried by the Worker with backoff; only one that failed
  // many times in a row is stopped (disabled_at), and that one needs the user.
  const retryingCount = subs.filter(s => s.feed.last_error && !s.feed.disabled_at).length
  const stoppedCount = subs.filter(s => s.feed.disabled_at).length
  const totalUnread = Object.values(unread).reduce((a, b) => a + b, 0)
  return <div className="shell">
    <aside className="sidebar">
      <div className="brand"><img className="brand-mark" src="/icon-192.png" alt="" /><div><strong>Gugu Reader</strong><small>{t.tagline}</small></div></div>
      <form className="search-form" onSubmit={runSearch}><input type="search" placeholder={t.searchPlaceholder} value={searchInput} onChange={e => setSearchInput(e.target.value)} required /></form>
      <div className="nav-group"><button className={folderID === null && subID === null ? 'active' : ''} onClick={() => { setFilter('all'); setFolderID(null); setSubID(null) }}>{t.allEntries}</button>
        <button className={filter === 'unread' ? 'active subtle' : ''} onClick={() => { setFilter('unread'); setFolderID(null); setSubID(null) }}>{t.unreadEntries}{totalUnread > 0 && <span className="count">{totalUnread}</span>}</button>
        <button className={filter === 'starred' ? 'active subtle' : ''} onClick={() => { setFilter('starred'); setFolderID(null); setSubID(null) }}>{t.starredEntries}</button>
        <button onClick={() => setChatOpen(true)}>{t.askGugu}</button></div>
      <div className="nav-heading heading-row"><span>{t.folders}</span><button onClick={() => setShowManage(true)}>{t.manage}</button></div>
      <div className="nav-group">{folders.map(f => <button key={f.id} className={folderID === f.id ? 'active' : ''} onClick={() => { setFolderID(f.id); setSubID(null) }}>{f.name}</button>)}</div>
      <form className="mini-form" onSubmit={createFolder}><input placeholder={t.newFolder} value={newFolder} onChange={e => setNewFolder(e.target.value)} /><button title={t.addFolder}>＋</button></form>
      <div className="nav-heading">{t.feeds}</div>
      <div className="nav-group feeds">{subs.map(s => <div className="feed-row" key={s.id}>
        <button className={subID === s.id ? 'active' : ''} onClick={() => { setSubID(s.id); setFolderID(null) }}>{s.feed.title || s.feed.url}</button>
        {unread[s.id] > 0 && <span className="count">{unread[s.id]}</span>}
        {s.feed.disabled_at && <span className="dead" title={t.feedDisabledTitle(s.feed.last_error)}>{t.feedDisabled}</span>}
        <button className="icon" title={t.refresh} onClick={() => refreshSubscription(s.id)}>↻</button>
        <button className="icon" title={t.unsubscribe} onClick={() => removeSubscription(s.id)}>×</button>
      </div>)}</div>
      <div className="side-bottom"><label className="import-label">{t.importOPML}<input type="file" accept=".opml,.xml,text/xml" onChange={e => e.target.files?.[0] && importOPML(e.target.files[0])} /></label><a href="/api/opml/export">{t.exportOPML}</a><button onClick={logout}>{t.logout} · {user.username}</button><div className="top-tools side-tools">{langButton}{githubLink}</div></div>
    </aside>
    <main className="content">
      <header className="topbar"><div><span className="eyebrow">YOUR READING SPACE</span><h1>{activeTitle}</h1>{pendingCount > 0 && <p className="fetch-status">{t.fetching(pendingCount)}</p>}{retryingCount > 0 && <p className="fetch-status">{t.fetchRetrying(retryingCount)}</p>}{stoppedCount > 0 && <p className="fetch-status error">{t.fetchStopped(stoppedCount)}</p>}</div><button className="outline" onClick={markRead}>{t.markAllRead}</button></header>
      <section className="add-panel"><form onSubmit={discover}><div><strong>{t.addTitle}</strong><p>{t.addHint}</p></div><input type="text" inputMode="url" placeholder="https://example.com" value={feedInput} onChange={e => setFeedInput(e.target.value)} required /><select value={addFolderID ?? ''} onChange={e => setAddFolderID(e.target.value ? Number(e.target.value) : null)}><option value="">{t.noFolder}</option>{folders.map(f => <option key={f.id} value={f.id}>{f.name}</option>)}</select><button className="primary" disabled={busy}>{t.discoverButton}</button></form>
        {candidates.length > 0 && <div className="candidates">{t.multipleFound}{candidates.map(x => <button key={x.url} onClick={() => addSubscription(x.url)}>{x.title || x.url}</button>)}</div>}</section>
      {error && <div className="message error">{error}<button onClick={() => setError('')}>×</button></div>}
      {notice && <div className="message notice">{notice}<button onClick={() => setNotice('')}>×</button></div>}
      <div className="filterbar">{(['all', 'unread', 'starred'] as Filter[]).map(f => <button key={f} className={filter === f ? 'selected' : ''} onClick={() => setFilter(f)}>{f === 'all' ? t.filterAll : f === 'unread' ? t.filterUnread : t.filterStarred}</button>)}<button className="reload" onClick={() => loadEntries().catch(report)}>{t.reloadList}</button></div>
      <div className="entry-list">{entries.map(entry => <article key={entry.id} className={`entry-card ${entry.read ? 'read' : ''}`}>
        <button className="entry-main" onClick={() => openEntry(entry)}><span className="entry-meta">{entry.feed_title || t.feedFallback} · {new Date(entry.sort_at).toLocaleDateString(t.locale)}</span><h2>{entry.title || t.untitled}</h2><span className="entry-link">{t.readArticle}</span></button>
        <button className={`star ${entry.starred_at ? 'on' : ''}`} title={entry.starred_at ? t.unstar : t.star} onClick={() => changeState(entry, { starred: !entry.starred_at })}>{entry.starred_at ? '★' : '☆'}</button>
      </article>)}{entries.length === 0 && <div className="empty">{filter === 'unread' ? t.emptyUnread : filter === 'starred' ? t.emptyStarred : t.emptyList}</div>}</div>
      {cursor && <button className="load-more" onClick={() => loadEntries(cursor).catch(report)}>{t.loadMore}</button>}
    </main>
    {selected && <Sheet key={selected.id} onClose={() => setSelected(null)}><span className="eyebrow">{selected.feed_title} · {new Date(selected.sort_at).toLocaleDateString(t.locale)}</span><h1>{selected.title}</h1><div className="reader-actions">{selected.url && <a href={selected.url} target="_blank" rel="noopener noreferrer">{t.readOriginal}</a>}<button onClick={() => changeState(selected, { read: !selected.read })}>{selected.read ? t.markUnread : t.markRead}</button><button onClick={() => changeState(selected, { starred: !selected.starred_at })}>{selected.starred_at ? t.unstar : t.star}</button></div><div className="article-body" dangerouslySetInnerHTML={{ __html: body?.id !== selected.id ? `<p>${t.loadingBody}</p>` : body.content || `<p>${t.noContent}</p>` }} /></Sheet>}
    {search && <Sheet onClose={() => setSearch(null)}><span className="eyebrow">SEARCH</span><h1>{t.searchTitle(search.q)}</h1><p className="search-hint">{t.searchHint(search.results.length)}</p>
      {search.results.map(r => <article key={r.entry_id} className="search-result"><span className="entry-meta">{r.feed_title || r.feed_url} · {new Date(r.sort_at).toLocaleDateString(t.locale)}</span><h2>{r.url ? <a href={r.url} target="_blank" rel="noopener noreferrer">{r.title || t.untitled}</a> : r.title || t.untitled}</h2>{r.summary && <p>{r.summary.length > 140 ? r.summary.slice(0, 140) + '…' : r.summary}</p>}{r.subscribed ? <span className="search-subscribed">{t.subscribed}</span> : <button className="outline" onClick={() => subscribeFromSearch(r)}>{t.subscribeBlog}</button>}</article>)}
      {search.results.length === 0 && <div className="empty">{t.noResults}</div>}</Sheet>}
    <Chat t={t} lang={lang} open={chatOpen} setOpen={setChatOpen} subscribedURLs={subscribedURLs} onSubscribe={addSubscription} />
    {showManage && <Sheet className="manage-panel" onClose={() => setShowManage(false)}><span className="eyebrow">ORGANIZE</span><h1>{t.manageTitle}</h1><h2>{t.folders}</h2>{folders.map(f => <div className="manage-row" key={f.id}><span>{f.name}</span><button onClick={() => renameFolder(f)}>{t.rename}</button><button onClick={() => deleteFolder(f)}>{t.delete}</button></div>)}<h2>{t.feeds}</h2>{subs.map(s => <div className="manage-row" key={s.id}><span>{s.feed.title || s.feed.url}</span><select value={s.folder_id ?? ''} onChange={e => moveSubscription(s, e.target.value ? Number(e.target.value) : null)}><option value="">{t.uncategorized}</option>{folders.map(f => <option key={f.id} value={f.id}>{f.name}</option>)}</select><button onClick={() => removeSubscription(s.id)}>{t.unsubscribe}</button></div>)}</Sheet>}
  </div>
}
