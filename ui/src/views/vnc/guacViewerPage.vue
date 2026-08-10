<!--
  Proxy-mode viewer (guacd) — renders RDP/VNC via Apache Guacamole.

  The gateway's /connect-guac/:id handler connects to guacd, performs the guacd
  handshake with server-held credentials, and relays the Guacamole instruction
  stream here. guacamole-common-js renders it; credentials never reach the
  browser. guacamole-common-js is loaded lazily (optional dependency) so the
  build stays green until it's installed and verified against a real guacd.
-->
<template>
    <div class="viewer">
        <div class="toolbar">
            <span class="status" :data-s="state.status">{{ statusText }}</span>
            <div class="right">
                <span class="badge">{{ $t('vnc.proxyBadge') }}</span>
                <a-button size="small" @click="sendCad" :disabled="state.status !== 'connected'">Ctrl+Alt+Del</a-button>
                <a-button size="small" @click="goBack">Back</a-button>
            </div>
        </div>
        <div ref="screen" class="screen" tabindex="0"></div>
        <div v-if="state.status === 'failed'" class="overlay">
            <div class="msg">
                <p>{{ state.detail || $t('vnc.connectFailed') }}</p>
                <p class="hint">Proxy (guacd) viewer is not yet verified end-to-end. See the release notes.</p>
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
let keyboard: any = null

const statusText = computed(() =>
    state.status === 'connected' ? t('vnc.connected')
        : state.status === 'failed' ? t('vnc.connectFailed')
            : t('vnc.rdpConnecting'))

const connect = async () => {
    state.status = 'connecting'
    try {
        const pkg = 'guacamole-common-js'
        const mod: any = await import(/* @vite-ignore */ pkg)
        const Guacamole = mod.default || mod
        const el = screen.value!
        const w = Math.max(el.clientWidth || 1280, 640)
        const h = Math.max(el.clientHeight || 800, 480)

        const tunnel = new Guacamole.WebSocketTunnel(`connect-guac/${props.id}`)
        client = new Guacamole.Client(tunnel)
        el.appendChild(client.getDisplay().getElement())

        client.onstatechange = (s: number) => {
            // 3 = CONNECTED, 5 = DISCONNECTED (Guacamole.Client.State)
            if (s === 3) state.status = 'connected'
            else if (s === 5 && state.status !== 'connected') state.status = 'failed'
        }
        client.onerror = (e: any) => { state.status = 'failed'; state.detail = e?.message || 'guacd error' }

        client.connect(`width=${w}&height=${h}&dpi=96`)

        // Input wiring.
        const display = client.getDisplay().getElement()
        const mouse = new Guacamole.Mouse(display)
        mouse.onmousedown = mouse.onmouseup = mouse.onmousemove = (s: any) => client.sendMouseState(s)
        keyboard = new Guacamole.Keyboard(document)
        keyboard.onkeydown = (k: number) => client.sendKeyEvent(1, k)
        keyboard.onkeyup = (k: number) => client.sendKeyEvent(0, k)
    } catch (e) {
        state.status = 'failed'
        state.detail = (e as Error)?.message || 'guacamole-common-js unavailable'
    }
}

const sendCad = () => {
    if (!client) return
    // Ctrl(65507)+Alt(65513)+Delete(65535)
    const keys = [65507, 65513, 65535]
    keys.forEach((k) => client.sendKeyEvent(1, k))
    keys.reverse().forEach((k) => client.sendKeyEvent(0, k))
}

const goBack = () => {
    try { client?.disconnect() } catch { /* ignore */ }
    location.hash = '#/vnc'
}

onMounted(connect)
onBeforeUnmount(() => {
    try { keyboard?.reset?.() } catch { /* ignore */ }
    try { client?.disconnect() } catch { /* ignore */ }
})
</script>

<style lang="scss" scoped>
.viewer { position: fixed; inset: 0; display: flex; flex-direction: column; background: #101216; color: #ddd; }
.toolbar { height: 42px; flex: 0 0 42px; display: flex; align-items: center; justify-content: space-between; padding: 0 14px; background: #2a2a2a; }
.toolbar .right { display: flex; gap: 10px; align-items: center; }
.badge { font-size: 12px; color: #58c06a; border: 1px solid #58c06a; border-radius: 4px; padding: 1px 7px; }
.status[data-s='connected'] { color: #67c23a; }
.status[data-s='failed'] { color: #f56565; }
.screen { flex: 1; overflow: hidden; }
.overlay { position: absolute; inset: 42px 0 0 0; display: flex; align-items: center; justify-content: center; }
.msg { max-width: 460px; text-align: center; }
.msg .hint { color: #999; font-size: 13px; }
</style>
