import { Container, getContainer } from '@cloudflare/containers'

// Runs the HomeLog Docker image in demo mode. The container's disk is
// ephemeral, which suits a demo: every cold start seeds a fresh Thai sample
// household (and demo mode also resets it every hour), so nothing a visitor
// does survives.
export class HomeLogContainer extends Container {
  defaultPort = 8080
  // Stop after 30 idle minutes; the next visit starts it again (a few
  // seconds) with fresh data.
  sleepAfter = '30m'

  constructor(ctx, env) {
    super(ctx, env)
    this.envVars = {
      DEMO_MODE: 'true',
      GIN_MODE: 'release',
      PORT: '8080',
      DB_PATH: '/app/data/homelog.db',
      TZ: 'Asia/Bangkok',
      // Served over HTTPS by Cloudflare, but the container only sees plain
      // HTTP from the Worker, so the cookie's Secure flag must be forced.
      COOKIE_SECURE: 'true',
      // Set with: npx wrangler secret put JWT_SECRET
      JWT_SECRET: env.JWT_SECRET,
      DEMO_GOATCOUNTER_SITE: env.DEMO_GOATCOUNTER_SITE || '',
    }
  }
}

export default {
  async fetch(request, env) {
    if (!env.JWT_SECRET) {
      return new Response('JWT_SECRET is not set. Run: npx wrangler secret put JWT_SECRET', { status: 500 })
    }
    // Pass the visitor's IP on: the server rate-limits per client IP, and
    // without this every visitor would share the Worker's single bucket.
    const headers = new Headers(request.headers)
    const ip = request.headers.get('CF-Connecting-IP')
    if (ip) headers.set('X-Forwarded-For', ip)
    headers.set('X-Forwarded-Proto', 'https')
    return getContainer(env.HOMELOG).fetch(new Request(request, { headers }))
  },
}
