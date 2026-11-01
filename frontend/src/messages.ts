// All text the page shows, in Chinese and English. `en` has the type of `zh`,
// so a missing or extra translation is a type error and fails the build.

export type Lang = 'zh' | 'en'

const zh = {
  locale: 'zh-CN', // <html lang> and date format
  switchLang: 'EN',
  switchLangTitle: 'Switch to English',
  requestFailed: (status: number) => `请求失败 (${status})`,
  withReason: (text: string, reason: string) => `${text}（${reason}）`,
  loading: '正在打开阅读器…',

  authHeadline: '让阅读重新安静下来。',
  authIntro: '订阅你信任的网站，在同一个地方读完它们。',
  username: '用户名',
  password: '密码',
  login: '登录',
  register: '创建账号',
  toRegister: '没有账号？注册',
  toLogin: '已有账号？登录',

  tagline: '读你想读的',
  searchPlaceholder: '搜索中文独立博客…',
  allEntries: '全部文章',
  unreadEntries: '未读文章',
  starredEntries: '我的收藏',
  folders: '文件夹',
  manage: '管理',
  newFolder: '新建文件夹',
  addFolder: '添加文件夹',
  feeds: '订阅源',
  feedDisabled: '已失效',
  feedDisabledTitle: (reason: string) => `已停止抓取：${reason}。点击刷新可重试`,
  refresh: '手动刷新',
  unsubscribe: '取消订阅',
  importOPML: '导入 OPML',
  exportOPML: '导出 OPML',
  logout: '退出',

  subscriptionFallback: '订阅',
  fetching: (n: number) => `正在抓取 ${n} 个订阅源，文章会自动出现…`,
  fetchFailed: (n: number) => `${n} 个订阅源抓取失败，可在订阅源旁点击刷新重试。`,
  markAllRead: '全部标为已读',
  addTitle: '添加订阅',
  addHint: '粘贴博客首页或 RSS 地址',
  noFolder: '不分类',
  discoverButton: '发现并订阅',
  multipleFound: '找到多个订阅地址：',
  filterAll: '全部',
  filterUnread: '未读',
  filterStarred: '收藏',
  reloadList: '刷新列表 ↻',
  feedFallback: '订阅源',
  untitled: '无标题',
  readArticle: '阅读文章 →',
  star: '收藏',
  unstar: '取消收藏',
  emptyList: '这里还没有文章。添加订阅后，Worker 会自动抓取。',
  loadMore: '加载更多',

  readOriginal: '阅读原文 ↗',
  markRead: '标为已读',
  markUnread: '标为未读',
  loadingBody: '正在加载…',
  noContent: '这个订阅源没有提供正文，请点击“阅读原文”。',

  searchTitle: (q: string) => `「${q}」`,
  searchHint: (n: number) => `在博客池和你订阅的文章里找到 ${n} 篇，按相关度排序。`,
  subscribed: '已订阅',
  subscribeBlog: '订阅这个博客',
  noResults: '没有找到相关文章。试试换个词，或者用空格分开多个词。',

  manageTitle: '管理订阅',
  rename: '重命名',
  delete: '删除',
  uncategorized: '未分类',

  subscribedNotice: '已添加订阅，Worker 将很快抓取文章。',
  refreshScheduled: '已安排刷新，新文章会自动出现。',
  markedRead: '已标记为已读。',
  imported: (added: number, skipped: number) => `导入 ${added} 个订阅，跳过 ${skipped} 个。`,
  folderNamePrompt: '文件夹名称',
  confirmDeleteFolder: (name: string) => `删除“${name}”？其中的订阅会变为未分类。`,
  confirmUnsubscribe: '取消这个订阅？收藏的文章会保留在「我的收藏」里。',

  guguName: '咕咕',
  guguTagline: '帮你找值得订阅的独立博客',
  askGugu: '问咕咕',
  guguOpenTitle: '和咕咕聊聊，找值得订阅的博客',
  newChat: '新对话',
  newChatTitle: '清空对话，从头开始',
  close: '关闭',
  guguHello: '咕！我是咕咕，一只送信的鸽子。',
  guguAsk: '想读点什么？告诉我你的兴趣，我去博客池里帮你找。',
  suggestions: ['推荐几个写 Go 后端的博客', '想看点生活类的博客', '我订阅的文章里有讲数据库的吗'],
  replyCut: '（回复中断了）',
  subscribedCheck: '已订阅 ✓',
  subscribe: '订阅',
  chatPlaceholder: '想读什么？（Enter 发送，Shift+Enter 换行）',
  send: '发送',

  // Keyed by the code the API returns with an error.
  errors: {
    invalid_username: '用户名只能包含字母、数字、_ 和 -',
    invalid_length: '用户名要 3～64 个字符，密码要 8～72 个字符',
    username_taken: '这个用户名已经有人用了',
    invalid_credentials: '用户名或密码不对',
    too_many_attempts: '尝试次数太多，请稍后再试',
    discover_failed: '没有找到订阅地址',
    invalid_url: '这个地址不能订阅',
    already_subscribed: '已经订阅过了',
    folder_exists: '已经有同名的文件夹了',
    refreshed_recently: '刚刚抓取过，请过几分钟再刷新',
    invalid_opml: 'OPML 文件无效',
    message_invalid: '消息不能为空，也不能超过 1000 字',
    chat_quota: '咕咕累了：24 小时内最多聊 50 条，晚点再来吧',
    chat_unavailable: '咕咕还没有配置好',
    chat_upstream: '咕咕暂时联系不上，请稍后再试',
    chat_interrupted: '回复中断了，请再试一次',
  },
}

export type Messages = typeof zh

const en: Messages = {
  locale: 'en-US',
  switchLang: '中文',
  switchLangTitle: '切换到中文',
  requestFailed: status => `Request failed (${status})`,
  withReason: (text, reason) => `${text} (${reason})`,
  loading: 'Opening the reader…',

  authHeadline: 'Make reading quiet again.',
  authIntro: 'Follow the sites you trust and read them all in one place.',
  username: 'Username',
  password: 'Password',
  login: 'Log in',
  register: 'Create account',
  toRegister: 'No account? Sign up',
  toLogin: 'Have an account? Log in',

  tagline: 'Read what you want',
  searchPlaceholder: 'Search Chinese indie blogs…',
  allEntries: 'All articles',
  unreadEntries: 'Unread',
  starredEntries: 'Starred',
  folders: 'Folders',
  manage: 'Manage',
  newFolder: 'New folder',
  addFolder: 'Add folder',
  feeds: 'Feeds',
  feedDisabled: 'Stopped',
  feedDisabledTitle: reason => `No longer fetched: ${reason}. Refresh to try again`,
  refresh: 'Refresh',
  unsubscribe: 'Unsubscribe',
  importOPML: 'Import OPML',
  exportOPML: 'Export OPML',
  logout: 'Log out',

  subscriptionFallback: 'Feed',
  fetching: n => `Fetching ${n} ${n === 1 ? 'feed' : 'feeds'}; articles will show up shortly…`,
  fetchFailed: n => `${n} ${n === 1 ? 'feed' : 'feeds'} could not be fetched. Use the refresh button next to a feed to retry.`,
  markAllRead: 'Mark all as read',
  addTitle: 'Add a feed',
  addHint: 'Paste a blog homepage or RSS address',
  noFolder: 'No folder',
  discoverButton: 'Find and subscribe',
  multipleFound: 'Found several feeds:',
  filterAll: 'All',
  filterUnread: 'Unread',
  filterStarred: 'Starred',
  reloadList: 'Reload ↻',
  feedFallback: 'Feed',
  untitled: 'Untitled',
  readArticle: 'Read →',
  star: 'Star',
  unstar: 'Unstar',
  emptyList: 'No articles yet. Once you add a feed, the worker fetches it automatically.',
  loadMore: 'Load more',

  readOriginal: 'Open original ↗',
  markRead: 'Mark as read',
  markUnread: 'Mark as unread',
  loadingBody: 'Loading…',
  noContent: 'This feed has no full text. Use “Open original”.',

  searchTitle: q => `“${q}”`,
  searchHint: n => `${n} ${n === 1 ? 'article' : 'articles'} from the blog pool and your feeds, most relevant first.`,
  subscribed: 'Subscribed',
  subscribeBlog: 'Subscribe to this blog',
  noResults: 'Nothing found. Try other words, or separate words with spaces. The blogs are in Chinese, so Chinese keywords work best.',

  manageTitle: 'Manage feeds',
  rename: 'Rename',
  delete: 'Delete',
  uncategorized: 'No folder',

  subscribedNotice: 'Subscribed. The worker will fetch articles shortly.',
  refreshScheduled: 'Refresh scheduled. New articles will show up automatically.',
  markedRead: 'Marked as read.',
  imported: (added, skipped) => `Imported ${added}, skipped ${skipped}.`,
  folderNamePrompt: 'Folder name',
  confirmDeleteFolder: name => `Delete “${name}”? Its feeds will move to no folder.`,
  confirmUnsubscribe: 'Unsubscribe from this feed? Starred articles stay in Starred.',

  guguName: 'Gugu',
  guguTagline: 'Finds Chinese indie blogs worth following',
  askGugu: 'Ask Gugu',
  guguOpenTitle: 'Chat with Gugu to find blogs worth following',
  newChat: 'New chat',
  newChatTitle: 'Clear the conversation and start over',
  close: 'Close',
  guguHello: 'Coo! I am Gugu, a messenger pigeon.',
  guguAsk: 'What would you like to read? Tell me your interests and I will look through the pool of Chinese indie blogs.',
  suggestions: ['Recommend blogs about Go backend development', 'Something about everyday life', 'Do any articles I follow talk about databases?'],
  replyCut: '(reply broke off)',
  subscribedCheck: 'Subscribed ✓',
  subscribe: 'Subscribe',
  chatPlaceholder: 'What would you like to read? (Enter to send, Shift+Enter for a new line)',
  send: 'Send',

  errors: {
    invalid_username: 'Usernames may contain only letters, numbers, _ and -',
    invalid_length: 'Usernames need 3–64 characters and passwords 8–72',
    username_taken: 'That username is taken',
    invalid_credentials: 'Wrong username or password',
    too_many_attempts: 'Too many attempts. Please try again later.',
    discover_failed: 'Could not find a feed',
    invalid_url: 'This address cannot be subscribed to',
    already_subscribed: 'You already follow this feed',
    folder_exists: 'A folder with this name already exists',
    refreshed_recently: 'Fetched a moment ago. Try again in a few minutes.',
    invalid_opml: 'Invalid OPML file',
    message_invalid: 'Messages must be 1–1000 characters',
    chat_quota: 'Gugu is tired: 50 messages per 24 hours. Come back later.',
    chat_unavailable: 'Gugu is not set up on this server',
    chat_upstream: 'Cannot reach Gugu right now. Please try again later.',
    chat_interrupted: 'The reply broke off. Please try again.',
  },
}

export const messages: Record<Lang, Messages> = { zh, en }

const storageKey = 'go_feed_lang'

// The saved choice wins; otherwise Chinese browsers get Chinese and every
// other language gets English.
export function initialLang(): Lang {
  try {
    const saved = localStorage.getItem(storageKey)
    if (saved === 'zh' || saved === 'en') return saved
  } catch { /* storage blocked: fall through */ }
  return navigator.language.toLowerCase().startsWith('zh') ? 'zh' : 'en'
}

export function saveLang(lang: Lang) {
  try { localStorage.setItem(storageKey, lang) } catch { /* not remembered, still switched */ }
}

// ApiError keeps the API's error code so the text can be chosen in the
// user's language when it is shown.
export class ApiError extends Error {
  constructor(public status: number, public code: string, message: string) { super(message) }
}

export async function apiError(res: Response): Promise<ApiError> {
  const body = await res.json().catch(() => ({}))
  return new ApiError(res.status, body.code || '', body.error || '')
}

// These codes carry a technical reason (from fetching or parsing) worth showing.
const withDetail = new Set(['discover_failed', 'invalid_url', 'invalid_opml'])

export function errorText(t: Messages, e: unknown): string {
  if (!(e instanceof ApiError)) return e instanceof Error ? e.message : String(e)
  const known = (t.errors as Record<string, string>)[e.code]
  if (!known) return e.message || t.requestFailed(e.status)
  return withDetail.has(e.code) && e.message ? t.withReason(known, e.message) : known
}
