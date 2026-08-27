<!--
  X (xpra) viewer — renders X apps via the xpra HTML5 client over the
  byte-transparent bridge. Run the xpra server with a raw TCP listener
  (`xpra start --bind-tcp=0.0.0.0:10000 --start=xterm`); the cloud tunnels the
  bytes to it and xpra-html5-client renders windows into the desktop element.

  Note: XpraWindowManager is a *basic abstraction* — createWindow() builds an
  offscreen <canvas> per window and draws into it, but never mounts it in the
  DOM (setDesktopElement is only used for input coordinate context). So this
  component subscribes to the same window events and mounts/positions each
  window's canvas itself, and wires pointer input through the WM.
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
let wm: any = null
let activeWid = -1
const canvases = new Map<number, HTMLCanvasElement>()

const statusText = computed(() =>
    state.status === 'connected' ? t('vnc.connected')
        : state.status === 'failed' ? t('vnc.connectFailed')
            : t('vnc.xpraConnecting'))

// Position a window's canvas inside the screen element, using the server's
// window geometry. The screen's own offset (toolbar height) is handled by
// making the canvas position:absolute within the relatively-positioned screen.
const placeCanvas = (canvas: HTMLCanvasElement, position?: number[]) => {
    const [x, y] = position || [0, 0]
    canvas.style.position = 'absolute'
    canvas.style.left = `${Math.max(0, x)}px`
    canvas.style.top = `${Math.max(0, y)}px`
    canvas.style.outline = 'none'
    canvas.style.imageRendering = 'pixelated'
}

const mountWindow = (attributes: any) => {
    if (!wm || !screen.value) return
    const managed = wm.getWindow(attributes.id)
    if (!managed || !managed.canvas) return
    const canvas = managed.canvas as HTMLCanvasElement
    placeCanvas(canvas, attributes.position)
    if (canvas.parentElement !== screen.value) screen.value.appendChild(canvas)
    canvases.set(attributes.id, canvas)
    activeWid = attributes.id
    wm.setActiveWindow(attributes.id)
}

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

        wm = new XpraWindowManager(xpra)
        wm.setDesktopElement(screen.value)
        wm.init()

        // The WM reads pointer coordinates as raw viewport clientX/clientY;
        // remap them to screen-relative (== server window coords for a
        // top-left window) so clicks land in the right place.
        xpra.mouse.getPosition = (ev: MouseEvent) => {
            const r = screen.value!.getBoundingClientRect()
            return [Math.round(ev.clientX - r.left), Math.round(ev.clientY - r.top)]
        }

        // Mount each window's canvas as the WM creates it. Our handler runs
        // after the WM's own createWindow (subscribed in init()), so
        // getWindow() already has the instance + canvas.
        xpra.on('newWindow', (attributes: any) => mountWindow(attributes))
        xpra.on('newTray', (attributes: any) => mountWindow(attributes))
        xpra.on('moveResizeWindow', (data: any) => {
            const canvas = canvases.get(data.wid)
            if (canvas && data.position) placeCanvas(canvas, data.position)
        })
        xpra.on('removeWindow', (wid: number) => {
            const canvas = canvases.get(wid)
            if (canvas && canvas.parentElement) canvas.parentElement.removeChild(canvas)
            canvases.delete(wid)
        })

        // Pointer input → active window. Keyboard is wired globally by the WM,
        // which only forwards keydown/keyup whose target is document.body — so
        // we must NOT move focus onto the screen/canvas (that would swallow
        // every keystroke). Clicking a <canvas> keeps focus on body already.
        const activeWin = () => (activeWid >= 0 ? wm.getWindow(activeWid) : null)
        screen.value!.addEventListener('mousedown', (ev) => wm.mouseButton(activeWin(), ev, true))
        screen.value!.addEventListener('mouseup', (ev) => wm.mouseButton(activeWin(), ev, false))
        screen.value!.addEventListener('mousemove', (ev) => wm.mouseMove(activeWin(), ev))
        screen.value!.addEventListener('contextmenu', (ev) => ev.preventDefault())

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
.screen { flex: 1; overflow: hidden; position: relative; background: #1e1e1e; outline: none; }
.overlay { position: absolute; inset: 42px 0 0 0; display: flex; align-items: center; justify-content: center; }
.msg { max-width: 460px; text-align: center; }
</style>
