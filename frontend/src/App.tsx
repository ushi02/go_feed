import { useCallback, useEffect, useMemo, useState } from 'react'
import Chat from './Chat'

type User = { id: number; username: string }
type Folder = { id: number; name: string }
type Feed = { id: number; title: string; url: string; last_error: string; last_fetched_at: string | null; disabled_at: string | null }
type Subscription = { id: number; feed_id: number; folder_id: number | null; feed: Feed }
type Entry = { id: number; feed_id: number; feed_title: string; title: string; url: string; sort_at: string; read: boolean; starred_at: string | null }
type Candidate = { url: string; title: string }
type SearchResult = { entry_id: number; title: string; url: string; summary: string; sort_at: string; feed_id: number; feed_title: string; feed_url: string; subscribed: boolean }
type Filter = 'all' | 'unread' | 'starred'

// feedState changes whenever the Worker finishes a fetch, successful or not.
const feedState = (s: Subscription) => `${s.feed.last_fetched_at}|${s.feed.last_error}`

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const res = await fetch(`/api${path}`, {
    credentials: 'include',
    headers: init.body instanceof FormData ? undefined : { 'Content-Type': 'application/json' },
    ...init,
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error || `请求失败 (${res.status})`)
  }
  // Some successful responses have no body (204, and 202 from refresh), so
  // parse JSON only when there is something to parse.
  const text = await res.text()
  return (text ? JSON.parse(text) : undefined) as T
}

export default function App() {
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
  const [searchInput, setSearchInput] = useState('')
  const [search, setSearch] = useState<{ q: string; results: SearchResult[] } | null>(null)

  const subscribedURLs = useMemo(() => new Set(subs.map(s => s.feed.url)), [subs])
  const report = (e: unknown) => { setError(e instanceof Error ? e.message : String(e)); setNotice('') }
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
      setFeedInput(''); setCandidates([]); setNotice('已添加订阅，Worker 将很快抓取文章。')
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
    const name = window.prompt('文件夹名称', folder.name)?.trim()
    if (!name || name === folder.name) return
    try { await request(`/folders/${folder.id}`, { method: 'PATCH', body: JSON.stringify({ name }) }); await refreshNav() } catch (e) { report(e) }
  }
  async function deleteFolder(folder: Folder) {
    if (!window.confirm(`删除“${folder.name}”？其中的订阅会变为未分类。`)) return
    try { await request(`/folders/${folder.id}`, { method: 'DELETE' }); if (folderID === folder.id) setFolderID(null); await refreshNav() } catch (e) { report(e) }
  }
  async function moveSubscription(sub: Subscription, target: number | null) {
    try { await request(`/subscriptions/${sub.id}`, { method: 'PATCH', body: JSON.stringify({ folder_id: target }) }); await refreshNav(); await loadEntries() } catch (e) { report(e) }
  }
  async function removeSubscription(id: number) {
    if (!window.confirm('取消这个订阅？收藏的文章会保留在「我的收藏」里。')) return
    try { await request(`/subscriptions/${id}`, { method: 'DELETE' }); if (subID === id) setSubID(null); await refreshNav(); await loadEntries() } catch (e) { report(e) }
  }
  async function refreshSubscription(id: number) {
    const sub = subs.find(s => s.id === id)
    try {
      await request(`/subscriptions/${id}/refresh`, { method: 'POST' }); setNotice('已安排刷新，新文章会自动出现。')
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
    try { await request('/entries/mark-read', { method: 'POST', body: JSON.stringify(scope) }); await loadEntries(); setNotice('已标记为已读。') } catch (e) { report(e) }
  }
  async function importOPML(file: File) {
    const form = new FormData(); form.append('file', file)
    try {
      const data = await request<{ added: number; skipped: number }>('/opml/import', { method: 'POST', body: form })
      setNotice(`导入 ${data.added} 个订阅，跳过 ${data.skipped} 个。`)
      await refreshNav()
    } catch (e) { report(e) }
  }

  if (!ready) return <div className="loading">正在打开阅读器…</div>
  if (!user) return <main className="auth-page"><section className="auth-card">
    <span className="eyebrow">GO FEED READER</span><h1>让阅读重新安静下来。</h1>
    <p>订阅你信任的网站，在同一个地方读完它们。</p>
    <form onSubmit={authenticate}>
      <label>用户名<input value={username} onChange={e => setUsername(e.target.value)} minLength={3} required /></label>
      <label>密码<input type="password" value={password} onChange={e => setPassword(e.target.value)} minLength={8} required /></label>
      <button className="primary" disabled={busy}>{loginMode === 'login' ? '登录' : '创建账号'}</button>
    </form>
    <button className="text-button" onClick={() => setLoginMode(loginMode === 'login' ? 'register' : 'login')}>{loginMode === 'login' ? '没有账号？注册' : '已有账号？登录'}</button>
    {error && <p className="error">{error}</p>}
  </section></main>

  const activeTitle = subID !== null ? subs.find(s => s.id === subID)?.feed.title || '订阅' : folderID !== null ? folders.find(f => f.id === folderID)?.name || '文件夹' : '全部文章'
  const pendingCount = subs.filter(s => !s.feed.last_fetched_at && !s.feed.last_error).length
  const failedCount = subs.filter(s => s.feed.last_error).length
  const totalUnread = Object.values(unread).reduce((a, b) => a + b, 0)
  return <div className="shell">
    <aside className="sidebar">
      <div className="brand"><span className="brand-mark">◉</span><div><strong>Go Feed</strong><small>读你想读的</small></div></div>
      <form className="search-form" onSubmit={runSearch}><input type="search" placeholder="搜索中文独立博客…" value={searchInput} onChange={e => setSearchInput(e.target.value)} required /></form>
      <div className="nav-group"><button className={folderID === null && subID === null ? 'active' : ''} onClick={() => { setFilter('all'); setFolderID(null); setSubID(null) }}>全部文章</button>
        <button className={filter === 'unread' ? 'active subtle' : ''} onClick={() => { setFilter('unread'); setFolderID(null); setSubID(null) }}>未读文章{totalUnread > 0 && <span className="count">{totalUnread}</span>}</button>
        <button className={filter === 'starred' ? 'active subtle' : ''} onClick={() => { setFilter('starred'); setFolderID(null); setSubID(null) }}>我的收藏</button></div>
      <div className="nav-heading heading-row"><span>文件夹</span><button onClick={() => setShowManage(true)}>管理</button></div>
      <div className="nav-group">{folders.map(f => <button key={f.id} className={folderID === f.id ? 'active' : ''} onClick={() => { setFolderID(f.id); setSubID(null) }}>{f.name}</button>)}</div>
      <form className="mini-form" onSubmit={createFolder}><input placeholder="新建文件夹" value={newFolder} onChange={e => setNewFolder(e.target.value)} /><button title="添加文件夹">＋</button></form>
      <div className="nav-heading">订阅源</div>
      <div className="nav-group feeds">{subs.map(s => <div className="feed-row" key={s.id}>
        <button className={subID === s.id ? 'active' : ''} onClick={() => { setSubID(s.id); setFolderID(null) }}>{s.feed.title || s.feed.url}</button>
        {unread[s.id] > 0 && <span className="count">{unread[s.id]}</span>}
        {s.feed.disabled_at && <span className="dead" title={`已停止抓取：${s.feed.last_error}。点击刷新可重试`}>已失效</span>}
        <button className="icon" title="手动刷新" onClick={() => refreshSubscription(s.id)}>↻</button>
        <button className="icon" title="取消订阅" onClick={() => removeSubscription(s.id)}>×</button>
      </div>)}</div>
      <div className="side-bottom"><label className="import-label">导入 OPML<input type="file" accept=".opml,.xml,text/xml" onChange={e => e.target.files?.[0] && importOPML(e.target.files[0])} /></label><a href="/api/opml/export">导出 OPML</a><button onClick={logout}>退出 · {user.username}</button></div>
    </aside>
    <main className="content">
      <header className="topbar"><div><span className="eyebrow">YOUR READING SPACE</span><h1>{activeTitle}</h1>{pendingCount > 0 && <p className="fetch-status">正在抓取 {pendingCount} 个订阅源，文章会自动出现…</p>}{failedCount > 0 && <p className="fetch-status error">{failedCount} 个订阅源抓取失败，可在订阅源旁点击刷新重试。</p>}</div><button className="outline" onClick={markRead}>全部标为已读</button></header>
      <section className="add-panel"><form onSubmit={discover}><div><strong>添加订阅</strong><p>粘贴博客首页或 RSS 地址</p></div><input type="text" inputMode="url" placeholder="https://example.com" value={feedInput} onChange={e => setFeedInput(e.target.value)} required /><select value={addFolderID ?? ''} onChange={e => setAddFolderID(e.target.value ? Number(e.target.value) : null)}><option value="">不分类</option>{folders.map(f => <option key={f.id} value={f.id}>{f.name}</option>)}</select><button className="primary" disabled={busy}>发现并订阅</button></form>
        {candidates.length > 0 && <div className="candidates">找到多个订阅地址：{candidates.map(x => <button key={x.url} onClick={() => addSubscription(x.url)}>{x.title || x.url}</button>)}</div>}</section>
      {error && <div className="message error">{error}<button onClick={() => setError('')}>×</button></div>}
      {notice && <div className="message notice">{notice}<button onClick={() => setNotice('')}>×</button></div>}
      <div className="filterbar">{(['all', 'unread', 'starred'] as Filter[]).map(f => <button key={f} className={filter === f ? 'selected' : ''} onClick={() => setFilter(f)}>{f === 'all' ? '全部' : f === 'unread' ? '未读' : '收藏'}</button>)}<button className="reload" onClick={() => loadEntries().catch(report)}>刷新列表 ↻</button></div>
      <div className="entry-list">{entries.map(entry => <article key={entry.id} className={`entry-card ${entry.read ? 'read' : ''}`}>
        <button className="entry-main" onClick={() => openEntry(entry)}><span className="entry-meta">{entry.feed_title || '订阅源'} · {new Date(entry.sort_at).toLocaleDateString('zh-CN')}</span><h2>{entry.title || '无标题'}</h2><span className="entry-link">阅读文章 →</span></button>
        <button className={`star ${entry.starred_at ? 'on' : ''}`} title={entry.starred_at ? '取消收藏' : '收藏'} onClick={() => changeState(entry, { starred: !entry.starred_at })}>{entry.starred_at ? '★' : '☆'}</button>
      </article>)}{entries.length === 0 && <div className="empty">这里还没有文章。添加订阅后，Worker 会自动抓取。</div>}</div>
      {cursor && <button className="load-more" onClick={() => loadEntries(cursor).catch(report)}>加载更多</button>}
    </main>
    {selected && <div className="reader-backdrop" onClick={() => setSelected(null)}><section className="reader" onClick={e => e.stopPropagation()}><button className="close" onClick={() => setSelected(null)}>×</button><span className="eyebrow">{selected.feed_title} · {new Date(selected.sort_at).toLocaleDateString('zh-CN')}</span><h1>{selected.title}</h1><div className="reader-actions">{selected.url && <a href={selected.url} target="_blank" rel="noopener noreferrer">阅读原文 ↗</a>}<button onClick={() => changeState(selected, { read: !selected.read })}>{selected.read ? '标为未读' : '标为已读'}</button><button onClick={() => changeState(selected, { starred: !selected.starred_at })}>{selected.starred_at ? '取消收藏' : '收藏'}</button></div><div className="article-body" dangerouslySetInnerHTML={{ __html: body?.id !== selected.id ? '<p>正在加载…</p>' : body.content || '<p>这个订阅源没有提供正文，请点击“阅读原文”。</p>' }} /></section></div>}
    {search && <div className="reader-backdrop" onClick={() => setSearch(null)}><section className="reader" onClick={e => e.stopPropagation()}><button className="close" onClick={() => setSearch(null)}>×</button><span className="eyebrow">SEARCH</span><h1>「{search.q}」</h1><p className="search-hint">在博客池和你订阅的文章里找到 {search.results.length} 篇，按相关度排序。</p>
      {search.results.map(r => <article key={r.entry_id} className="search-result"><span className="entry-meta">{r.feed_title || r.feed_url} · {new Date(r.sort_at).toLocaleDateString('zh-CN')}</span><h2>{r.url ? <a href={r.url} target="_blank" rel="noopener noreferrer">{r.title || '无标题'}</a> : r.title || '无标题'}</h2>{r.summary && <p>{r.summary.length > 140 ? r.summary.slice(0, 140) + '…' : r.summary}</p>}{r.subscribed ? <span className="search-subscribed">已订阅</span> : <button className="outline" onClick={() => subscribeFromSearch(r)}>订阅这个博客</button>}</article>)}
      {search.results.length === 0 && <div className="empty">没有找到相关文章。试试换个词，或者用空格分开多个词。</div>}</section></div>}
    <Chat subscribedURLs={subscribedURLs} onSubscribe={addSubscription} />
    {showManage && <div className="reader-backdrop" onClick={() => setShowManage(false)}><section className="reader manage-panel" onClick={e => e.stopPropagation()}><button className="close" onClick={() => setShowManage(false)}>×</button><span className="eyebrow">ORGANIZE</span><h1>管理订阅</h1><h2>文件夹</h2>{folders.map(f => <div className="manage-row" key={f.id}><span>{f.name}</span><button onClick={() => renameFolder(f)}>重命名</button><button onClick={() => deleteFolder(f)}>删除</button></div>)}<h2>订阅源</h2>{subs.map(s => <div className="manage-row" key={s.id}><span>{s.feed.title || s.feed.url}</span><select value={s.folder_id ?? ''} onChange={e => moveSubscription(s, e.target.value ? Number(e.target.value) : null)}><option value="">未分类</option>{folders.map(f => <option key={f.id} value={f.id}>{f.name}</option>)}</select><button onClick={() => removeSubscription(s.id)}>取消订阅</button></div>)}</section></div>}
  </div>
}
