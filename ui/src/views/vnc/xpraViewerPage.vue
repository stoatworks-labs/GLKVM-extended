<!--
  X (xpra) viewer (EXPERIMENTAL, unverified).

  Architecture: identical byte-transparent model to VNC. Run the xpra server
  with a raw TCP listener (`xpra start --bind-tcp=0.0.0.0:10000 --start=xterm`)
  and point an endpoint at it. The cloud tunnels the bytes; the browser runs
  the xpra HTML5 client, which speaks the xpra protocol (its websocket payloads
  are raw xpra-protocol bytes, which our bridge forwards to bind-tcp).

  Status: NOT yet verified end-to-end — needs a running xpra server to confirm
  the client wiring and codec worker setup. Loaded lazily so the build stays
  green without the optional client dependency installed.
-->
<template>
    <div class="viewer">
        <div class="toolbar">
            <span class="status" :data-s="state.status">{{ statusText }}</span>
            <div class="right">
                <span class="badge">X · experimental</span>
                <a-button size="small" @click="goBack">Back</a-button>
            </div>
        </div>
        <div ref="screen" class="screen"></div>
        <div v-if="state.status === 'failed'" class="overlay">
            <div class="msg">
                <p>{{ state.detail || $t('vnc.unsupported') }}</p>
                <p class="hint">X (xpra) viewer integration is not yet verified. See the release notes.</p>
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
            : t('vnc.xpraConnecting'))

const wsUrl = () => {
    const proto = location.protocol === 'https:' ? 'wss://' : 'ws://'
    return proto + location.host + `/connect-vnc/${props.id}`
}

const connect = async () => {
    state.status = 'connecting'
    try {
        // Optional dependency; installed + verified during the integration
        // spike. Variable specifier => runtime import, not resolved at build.
        const pkg = 'xpra-html5-client'
        const mod: any = await import(/* @vite-ignore */ pkg)
        const XpraClient = mod.XpraClient || mod.default?.XpraClient
        if (!XpraClient) throw new Error('xpra client API not wired')
        client = new XpraClient()
        // The exact connection + canvas mount is finalised against a real
        // xpra server during the spike.
        if (typeof client.connect === 'function') {
            await client.connect({ uri: wsUrl() })
            state.status = 'connected'
        } else {
            throw new Error('xpra client connect() not found')
        }
    } catch (e) {
        state.status = 'failed'
        state.detail = (e as Error)?.message || 'xpra client unavailable'
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
.screen { flex: 1; overflow: hidden; background: #000; }
.overlay { position: absolute; inset: 42px 0 0 0; display: flex; align-items: center; justify-content: center; }
.msg { max-width: 460px; text-align: center; }
.msg .hint { color: #999; font-size: 13px; }
</style>
