<!--
  RDP viewer (client-side) — runs IronRDP-web (WASM) in the browser and speaks
  RDP over a Devolutions Gateway sidecar via RDCleanPath.

  Credentials are entered here and travel inside the RDP/TLS/CredSSP stream to
  the server; the cloud never sees them. The cloud only mints a short-lived
  gateway association token (POST /api/vnc-endpoints/:id/rdp-session) that
  authorises the transport to this endpoint's address.

  Flow: creds form -> fetch token -> init WASM backend -> mount
  <iron-remote-desktop> -> configBuilder(...).connect().
-->
<template>
    <div class="viewer">
        <div class="toolbar">
            <span class="status" :data-s="state.status">{{ statusText }}</span>
            <div class="right">
                <span class="badge">RDP · client</span>
                <a-button size="small" @click="goBack">Back</a-button>
            </div>
        </div>

        <!-- Credentials form (client-side; password never stored) -->
        <div v-if="state.status === 'form'" class="formwrap">
            <div class="card">
                <h3>Connect to {{ destinationLabel }}</h3>
                <a-input v-model:value="creds.username" placeholder="Username" class="fld" @pressEnter="start" />
                <a-input v-model:value="creds.domain" placeholder="Domain (optional)" class="fld" @pressEnter="start" />
                <a-input-password v-model:value="creds.password" placeholder="Password" class="fld" @pressEnter="start" />
                <a-button type="primary" block :loading="state.busy" @click="start">Connect</a-button>
                <p v-if="state.detail" class="err">{{ state.detail }}</p>
            </div>
        </div>

        <div v-show="state.status === 'connecting' || state.status === 'connected'" ref="screen" class="screen"></div>

        <div v-if="state.status === 'failed'" class="overlay">
            <div class="msg">
                <p>{{ state.detail || $t('vnc.connectFailed') }}</p>
                <a-button size="small" @click="backToForm">Try again</a-button>
            </div>
        </div>
    </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { reqRdpSession, type RdpSession } from '@/api/vncEndpoint'

const props = defineProps({ id: { type: String, required: true } })
const { t } = useI18n()

const screen = ref<HTMLElement | null>(null)
const state = reactive({
    status: 'form' as 'form' | 'connecting' | 'connected' | 'failed',
    busy: false,
    detail: '',
})
const creds = reactive({ username: '', domain: '', password: '' })
const destinationLabel = ref('')
let el: any = null
let ui: any = null

const statusText = computed(() =>
    state.status === 'connected' ? t('vnc.connected')
        : state.status === 'failed' ? t('vnc.connectFailed')
            : state.status === 'connecting' ? t('vnc.rdpConnecting')
                : 'RDP')

const backToForm = () => { state.status = 'form'; state.detail = '' }

const start = async () => {
    if (state.busy) return
    if (!creds.username) { state.detail = 'Username is required'; return }
    state.busy = true
    state.detail = ''
    try {
        // 1. Mint the gateway association token (authorises transport only).
        const res = await reqRdpSession(Number(props.id))
        const session = res.data as RdpSession
        destinationLabel.value = session.destination

        state.status = 'connecting'

        // 2. Lazy-load the web component + WASM RDP backend. The backend WASM
        //    is inlined in the JS, so no extra asset handling is needed. The
        //    WASM must be initialised (backend.init()) before its classes are
        //    usable, and the web component expects the `Backend` object
        //    ({SessionBuilder, DesktopSize, DeviceEvent, ...}) — not the whole
        //    module namespace — assigned to `.module`.
        const [, backend] = await Promise.all([
            import('@devolutions/iron-remote-desktop'),
            import('@devolutions/iron-remote-desktop-rdp'),
        ])
        // init(logFilter) loads the inlined WASM and calls the module's
        // setup(); it REQUIRES a string (the tracing log filter) — calling it
        // with no argument makes the glue read undefined.length.
        await (backend as any).init('info')

        // 3. Mount the element and wire the ready -> connect handshake.
        if (!screen.value) throw new Error('viewport not ready')
        el = document.createElement('iron-remote-desktop')
        el.setAttribute('scale', 'fit')
        el.setAttribute('flexcenter', 'true')
        el.style.width = '100%'
        el.style.height = '100%'
        ;(el as any).module = (backend as any).Backend
        screen.value.innerHTML = ''
        screen.value.appendChild(el)

        el.addEventListener('ready', async (ev: any) => {
            try {
                ui = ev.detail.irgUserInteraction
                // Negotiate an explicit desktop size: the host element can be
                // 0×0 at connect time (flex/hidden), which would otherwise
                // negotiate a degenerate resolution and get reset by the server.
                const rect = screen.value?.getBoundingClientRect()
                const width = Math.max(1024, Math.round(rect?.width || 1280))
                const height = Math.max(720, Math.round(rect?.height || 720))
                const builder = ui.configBuilder()
                    .withUsername(creds.username)
                    .withPassword(creds.password)
                    .withDestination(session.destination)
                    .withProxyAddress(session.proxyAddress)
                    .withAuthToken(session.authToken)
                    .withDesktopSize({ width, height })
                if (creds.domain) builder.withServerDomain(creds.domain)
                const config = builder.build()
                const sessionInfo = await ui.connect(config)
                state.status = 'connected'
                // The component starts hidden; reveal it now that a session is live.
                try { ui.setVisibility?.(true) } catch { /* ignore */ }
                // connect() does NOT start the session — the caller must drive
                // the read/render loop via run(). It resolves when the session
                // ends (without it, no graphics are consumed and the server
                // resets the idle connection after ~38s).
                Promise.resolve(sessionInfo?.run?.())
                    .then((info: any) => {
                        state.status = 'failed'
                        state.detail = info?.reason?.() || 'Session ended'
                    })
                    .catch((e: any) => {
                        state.status = 'failed'
                        state.detail = e?.message || 'Session ended'
                    })
            } catch (e) {
                // eslint-disable-next-line no-console
                console.error('RDP connect error', e)
                state.status = 'failed'
                state.detail = (e as Error)?.message || 'RDP connection failed'
            }
        })
    } catch (e) {
        // eslint-disable-next-line no-console
        console.error('RDP setup error', e)
        state.status = 'failed'
        state.detail = (e as any)?.message || (e as any)?.msg || 'RDP session setup failed'
    } finally {
        state.busy = false
    }
}

const disconnect = () => {
    try { ui?.shutdown?.() } catch { /* ignore */ }
    try { el?.remove?.() } catch { /* ignore */ }
    ui = null; el = null
}

const goBack = () => { disconnect(); location.hash = '#/vnc' }

onMounted(() => { /* wait for creds */ })
onBeforeUnmount(disconnect)
</script>

<style lang="scss" scoped>
.viewer { position: fixed; inset: 0; display: flex; flex-direction: column; background: #101216; color: #ddd; }
.toolbar { height: 42px; flex: 0 0 42px; display: flex; align-items: center; justify-content: space-between; padding: 0 14px; background: #2a2a2a; }
.toolbar .right { display: flex; gap: 10px; align-items: center; }
.badge { font-size: 12px; color: #40a9ff; border: 1px solid #40a9ff; border-radius: 4px; padding: 1px 7px; }
.status[data-s='connected'] { color: #67c23a; }
.status[data-s='failed'] { color: #f56565; }
.screen { flex: 1; overflow: hidden; position: relative; background: #000; }
.formwrap { flex: 1; display: flex; align-items: center; justify-content: center; }
.card { width: 340px; background: #1c1f26; border: 1px solid #2c313c; border-radius: 10px; padding: 22px; }
.card h3 { margin: 0 0 16px; font-size: 15px; color: #eee; }
.fld { margin-bottom: 12px; }
.err { margin: 12px 0 0; color: #f56565; font-size: 13px; }
.overlay { position: absolute; inset: 42px 0 0 0; display: flex; align-items: center; justify-content: center; }
.msg { max-width: 460px; text-align: center; display: flex; flex-direction: column; gap: 12px; align-items: center; }
</style>
