<!--
  RDP viewer (EXPERIMENTAL, unverified).

  Architecture: the cloud /connect-vnc/:id bridge is byte-transparent, so it
  tunnels raw TCP:3389 the same way it tunnels VNC. The browser side runs an
  RDP-in-WASM client (IronRDP) that speaks the full RDP protocol — including
  TLS and NLA — over the websocket, entering credentials in-browser.

  Status: NOT yet verified end-to-end. Two open items before this is trusted:
    1. Bundle the WASM client. The Devolutions web component
       (@devolutions/iron-remote-gui) historically expects a Devolutions
       Gateway handshake; the lower-level ironrdp-web speaks raw RDP over a
       plain proxy (as electerm/ironrdp-wasm uses). We need the plain-proxy
       path so our byte-bridge is enough — this requires a spike against a
       real RDP server.
    2. Decide credential handling: client-side (here) vs a server-side RDP
       proxy that keeps NLA creds off the browser.

  This page loads the client lazily and shows a clear status; it does not
  pretend to work until the spike above is done.
-->
<template>
    <div class="viewer">
        <div class="toolbar">
            <span class="status" :data-s="state.status">{{ statusText }}</span>
            <div class="right">
                <span class="badge">RDP · experimental</span>
                <a-button size="small" @click="goBack">{{ $t('vnc.reconnect') === '' ? 'Back' : 'Back' }}</a-button>
            </div>
        </div>
        <div ref="screen" class="screen"></div>
        <div v-if="state.status === 'failed'" class="overlay">
            <div class="msg">
                <p>{{ state.detail || $t('vnc.unsupported') }}</p>
                <p class="hint">RDP viewer integration is not yet verified. See the release notes.</p>
            </div>
        </div>
    </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps({ id: { type: String, required: true } })
const { t } = useI18n()

const screen = ref<HTMLElement | null>(null)
const state = reactive({ status: 'connecting' as 'connecting' | 'connected' | 'failed', detail: '' })
let client: any = null

const statusText = computed(() =>
    state.status === 'connected' ? t('vnc.connected')
        : state.status === 'failed' ? t('vnc.connectFailed')
            : t('vnc.rdpConnecting'))

const wsUrl = () => {
    const proto = location.protocol === 'https:' ? 'wss://' : 'ws://'
    return proto + location.host + `/connect-vnc/${props.id}`
}

const connect = async () => {
    state.status = 'connecting'
    try {
        // Lazy, build-safe load: the WASM RDP client is an optional dependency
        // that must be installed and verified before this path works. The
        // specifier is a variable so the bundler treats it as a runtime import
        // rather than trying to resolve it at build time.
        const pkg = '@devolutions/iron-remote-gui'
        const mod: any = await import(/* @vite-ignore */ pkg)
        // The component self-registers <iron-remote-gui>; instantiate + connect.
        if (!screen.value) return
        const el: any = document.createElement('iron-remote-gui')
        screen.value.appendChild(el)
        client = el
        // API is intentionally minimal here; the exact connect signature is
        // finalised during the integration spike against a real RDP server.
        if (typeof el.connect === 'function') {
            await el.connect({ websocketUrl: wsUrl() })
            state.status = 'connected'
        } else {
            throw new Error('RDP client API not wired')
        }
        void mod
    } catch (e) {
        state.status = 'failed'
        state.detail = (e as Error)?.message || 'RDP client unavailable'
    }
}

const goBack = () => {
    try { client?.disconnect?.() } catch { /* ignore */ }
    location.hash = '#/vnc'
}

onMounted(connect)
onBeforeUnmount(() => { try { client?.disconnect?.() } catch { /* ignore */ } })
</script>

<style lang="scss" scoped>
.viewer { position: fixed; inset: 0; display: flex; flex-direction: column; background: #101216; color: #ddd; }
.toolbar { height: 42px; flex: 0 0 42px; display: flex; align-items: center; justify-content: space-between; padding: 0 14px; background: #2a2a2a; }
.toolbar .right { display: flex; gap: 10px; align-items: center; }
.badge { font-size: 12px; color: #f0a020; border: 1px solid #f0a020; border-radius: 4px; padding: 1px 7px; }
.status[data-s='connected'] { color: #67c23a; }
.status[data-s='failed'] { color: #f56565; }
.screen { flex: 1; overflow: hidden; }
.overlay { position: absolute; inset: 42px 0 0 0; display: flex; align-items: center; justify-content: center; }
.msg { max-width: 460px; text-align: center; }
.msg .hint { color: #999; font-size: 13px; }
</style>
