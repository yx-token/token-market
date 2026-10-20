/** 旧 /console 书签 → /admin */
export function consolePathToAdmin(path: string): string {
  if (path === '/console' || path === '/console/') return '/admin'
  if (path.startsWith('/console/')) return '/admin' + path.slice('/console'.length)
  return '/admin'
}
