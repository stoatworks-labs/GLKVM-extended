<!--
  X (xpra) viewer — renders X apps via the xpra HTML5 client over the
  byte-transparent bridge. Run the xpra server with a raw TCP listener
  (`xpra start --bind-tcp=0.0.0.0:10000 --start=xterm`); the cloud tunnels the
  bytes to it and xpra-html5-client renders windows into the desktop element.
-->
<template>
    <div class="viewer">
        <div class="toolbar">
            <span class="status" :data-s="state.status">{{ statusText }}</span>
            <div class="right">
                <span class="badge">X · xpra</span>
                <a-button size="small" @click="goBack">Back</a-button>
            </div>
        </div>
        <div ref="screen" class="screen"></div>
        <div v-if="state.status === 'failed'" class="overlay">
            <div class="msg"><p>{{ state.detail || $t('vnc.connectFailed') }}</p></div>
        </div>
    </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import XpraPacketWorker from '@/xpra/packet-worker.ts?worker'
import XpraDecodeWorker from '@/xpra/decode-worker.ts?worker'
import { XpraClient, XpraWindowManager } from 'xpra-html5-client'

const props = defineProps({ id: { type: String, required: true } })
const { t } = useI18n()

const screen = ref<HTMLElement | null>(null)
const state = reactive({ status: 'connecting' as 'connecting' | 'connected' | 'failed', detail: '' })
let xpra: any = null

const statusText = computed(() =>
    state.status === 'connected' ? t('vnc.connected')
        : state.status === 'failed' ? t('vnc.connectFailed')
            : t('vnc.xpraConnecting'))

const connect = async () => {
    state.status = 'connecting'
    try {
        const worker = new XpraPacketWorker()
        const decoder = new XpraDecodeWorker()
        xpra = new XpraClient({ worker, decoder })
        await xpra.init()

        xpra.on('connect', () => { state.status = 'connected' })
        xpra.on('sessionStarted', () => { state.status = 'connected' })
        xpra.on('disconnect', () => { if (state.status !== 'connected') state.status = 'failed' })
        xpra.on('error', (message: string) => { state.status = 'failed'; state.detail = message })

        const wm = new XpraWindowManager(xpra)
        wm.setDesktopElement(screen.value)
        wm.init()

        const proto = location.protocol === 'https:' ? 'wss://' : 'ws://'
        xpra.connect(`${proto}${location.host}/connect-vnc/${props.id}`, {})
    } catch (e) {
        state.status = 'failed'
        state.detail = (e as Error)?.message || 'xpra client failed to initialise'
    }
}

const goBack = () => {
    try { xpra?.disconnect() } catch { /* ignore */ }
    location.hash = '#/vnc'
}

onMounted(connect)
onBeforeUnmount(() => { try { xpra?.disconnect() } catch { /* ignore */ } })
</script>

<style lang="scss" scoped>
.viewer { position: fixed; inset: 0; display: flex; flex-direction: column; background: #101216; color: #ddd; }
.toolbar { height: 42px; flex: 0 0 42px; display: flex; align-items: center; justify-content: space-between; padding: 0 14px; background: #2a2a2a; }
.toolbar .right { display: flex; gap: 10px; align-items: center; }
.badge { font-size: 12px; color: #f0a020; border: 1px solid #f0a020; border-radius: 4px; padding: 1px 7px; }
.status[data-s='connected'] { color: #67c23a; }
.status[data-s='failed'] { color: #f56565; }
.screen { flex: 1; overflow: hidden; position: relative; background: #1e1e1e; }
.overlay { position: absolute; inset: 42px 0 0 0; display: flex; align-items: center; justify-content: center; }
.msg { max-width: 460px; text-align: center; }
</style>
