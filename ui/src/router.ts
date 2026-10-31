import { useEffect, useState } from 'react'

export interface Route {
  path: string
  segments: string[]
  hash: string
}

function parseHash(): Route {
  let hash = window.location.hash.replace(/^#/, '')
  if (!hash || hash === '/') hash = '/home'
  const [path] = hash.split('?')
  return { path, segments: path.split('/').filter(Boolean), hash }
}

export function useRoute(): Route {
  const [route, setRoute] = useState<Route>(parseHash)
  useEffect(() => {
    const onChange = () => setRoute(parseHash())
    window.addEventListener('hashchange', onChange)
    if (!window.location.hash) window.location.replace('#/home')
    return () => window.removeEventListener('hashchange', onChange)
  }, [])
  return route
}

export function navigate(path: string) {
  const next = path.startsWith('#') ? path : `#${path}`
  if (window.location.hash === next) return
  window.location.hash = next
}

export function href(path: string) {
  return `#${path}`
}
